package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/energon"
	"github.com/go-batteries/optimux/src/mediahose"
	"github.com/go-batteries/optimux/src/shared"

	appcfg "github.com/go-batteries/optimux/src/config"
)

var (
	s3Client *s3.Client
	region   = "us-east-1"

	appEnv string
	defaultBucket string
)

func init() {
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithDefaultRegion(region))
	if err != nil {
		log.Fatal("failed to initialize config")
	}

	s3Client = s3.NewFromConfig(cfg)

	regionFromEnv := strings.TrimSpace(os.Getenv("AWS_REGION"))
	if regionFromEnv != "" {
		region = regionFromEnv
	}

	appEnv = strings.TrimSpace(os.Getenv("ENVIRONMENT"))
	if appEnv == "" {
		appEnv = "stg"
	}

	defaultBucket = strings.TrimSpace(os.Getenv("DEFAULT_BUCKET"))
	if defaultBucket == "" {
		log.Fatal("default bucket not set")
	}
}

// VideoLambdaHandler handles video processing in lambda environment
func videoLambdaHandler(ctx context.Context, sqsEvent events.SQSEvent) error {
	log.Println("running video worker for env", appEnv)

	ctx, cancel := context.WithTimeout(ctx, 12*time.Minute)
	defer cancel()

	shared.MustSetupInstrumenter(&appcfg.Config{Env: appEnv})

	log.Println("processing video events for region", region)

	// Create encoder for S3 uploads
	encoder := encoders.NewS3Uploader(s3Client)

	// Create worker and dispatcher for batch processing
	worker := &mediahose.BatchWorker{
		Idx:         time.Now().UTC().Unix(),
		CloserChan:  make(chan int64),
		ConsumeResp: true,
	}

	lambdaRecords := []*energon.LambdaRecord{}

	// Define path patterns for video uploads based on environment
	// stg/videos/usr_* or prod/videos/usr_*
	videoPattern := shared.TemplateToRegex(fmt.Sprintf("%s/videos/usr_{usr_id}/*", appEnv))

	for _, msg := range sqsEvent.Records {
		var s3Event events.S3Event

		if !strings.EqualFold(region, msg.AWSRegion) {
			// Don't process videos from different region
			continue
		}

		if err := json.Unmarshal([]byte(msg.Body), &s3Event); err != nil {
			log.Printf("failed to parse S3 event from SQS message: %v", err)
			continue
		}

		log.Printf("sqs msg body %+v", s3Event)

		for _, record := range s3Event.Records {
			bucket := record.S3.Bucket.Name
			key := shared.MaybeDecodeURI(record.S3.Object.Key)
			
			// Check if this is a video file
			if !isVideoFile(key) {
				log.Printf("Skipping non-video file: %s", key)
				continue
			}

			// Check if path matches the expected pattern
			params := shared.ExtractParams(videoPattern, key)
			if len(params) == 0 {
				log.Printf("Skipping video not matching pattern: %s", key)
				continue
			}

			// Skip already processed files to avoid infinite loops
			if isProcessedFile(key) {
				log.Printf("Skipping already processed file: %s", key)
				continue
			}

			// Extract user ID from path pattern
			usrID := params["usr_id"]
			
			// Create lambda record for video processing
			wctx, wcancel := context.WithCancel(ctx)
			
			lambdaRecord := &energon.LambdaRecord{
				Bucket:          bucket,
				UploadedPath:    key,
				RequestedFormat: "", // Will be determined by job type
				Region:          region,
				BatchID:         usrID,
				Ctx:             wctx,
				Cancel:          wcancel,
			}
			
			lambdaRecords = append(lambdaRecords, lambdaRecord)
		}
	}

	if len(lambdaRecords) == 0 {
		log.Println("No video records to process")
		return nil
	}

	// Create batch queue and dispatcher
	batchQueue := make(chan *mediahose.BatchedJob, len(lambdaRecords))
	defer close(batchQueue)

	dispatcher := mediahose.NewDispatcher(100*time.Millisecond, batchQueue, mediahose.NoOpOnComplete)

	log.Println("Running dispatcher for batch job")
	dispatcher.RunInBackground(ctx)

	go worker.Work(ctx, batchQueue)

	// Create video processor handler
	videoProcessor := &energon.VideoProcessorHandler{
		S3Client:   s3Client,
		Worker:     worker,
		BatchQueue: batchQueue,
		Encoder:    encoder.Upload,
		Dispatcher: dispatcher,
	}

	dones := []shared.MailBoxCh{}
	videoMetadata := make(map[string]*energon.VideoProcessingResult) // videoKey -> result

	// Process each video record
	for _, record := range lambdaRecords {
		log.Printf("Processing video record: %+v", record)
		
		_dones, err := videoProcessor.Handle(ctx, record)
		if err != nil {
			log.Printf("Failed to handle video: %v", err)
			continue
		}
		
		dones = append(dones, _dones...)
		
		// Initialize metadata result for this video
		videoID := extractVideoID(record.UploadedPath)
		params := shared.ExtractParams(videoPattern, record.UploadedPath)
		usrID := params["usr_id"]
		
		videoMetadata[record.UploadedPath] = &energon.VideoProcessingResult{
			VideoID: videoID,
			UserID:  usrID,
			Env:     appEnv,
		}
	}

	log.Printf("Waiting for %d jobs to complete", len(dones))

	// Collect results from mailboxes
	for _, done := range dones {
		res, ok := <-done
		if !ok {
			continue
		}
		
		log.Printf("Job completed: %+v", res)
		// Results are collected but metadata update will be done separately
	}

	// Update S3 metadata for all processed videos
	for videoKey, result := range videoMetadata {
		log.Printf("Updating S3 metadata for: %s", videoKey)
		
		if err := energon.UpdateVideoS3Metadata(ctx, s3Client, defaultBucket, videoKey, result); err != nil {
			log.Printf("⚠️  Failed to update metadata for %s: %v", videoKey, err)
			// Don't fail the entire batch if metadata update fails
			continue
		}
	}

	log.Printf("Successfully processed %d video records with %d jobs", len(lambdaRecords), len(dones))
	return nil
}

// isVideoFile checks if the file is a video based on extension
func isVideoFile(key string) bool {
	for ext := range shared.VideoExtMap {
		if strings.HasSuffix(strings.ToLower(key), ext) {
			return true
		}
	}
	return false
}

// isProcessedFile checks if the file is already a processed output
func isProcessedFile(key string) bool {
	// Skip files in processed directory to avoid infinite loops
	return strings.Contains(key, "/processed/")
}

// extractVideoID extracts video ID from S3 key
func extractVideoID(key string) string {
	// Extract filename without extension as video ID
	parts := strings.Split(key, "/")
	filename := parts[len(parts)-1]
	
	// Remove extension
	if dotIndex := strings.LastIndex(filename, "."); dotIndex != -1 {
		filename = filename[:dotIndex]
	}
	
	return filename
}


func main() {
	lambda.Start(videoLambdaHandler)
}
