package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/davidbyttow/govips/v2/vips"
	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/energon"
	"github.com/go-batteries/optimux/src/mediahose"
	"github.com/go-batteries/optimux/src/services/mediametadata"
	"github.com/go-batteries/optimux/src/shared"

	appcfg "github.com/go-batteries/optimux/src/config"
)

var (
	s3Client *s3.Client
	region   = "us-east-1"

	appEnv string
	dbURL  string

	defaultBucket string // TODO: remove this
)

var vipsInit sync.Once

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

	dbURL = strings.TrimSpace(os.Getenv("PG_DBURL"))
	if dbURL == "" {
		log.Fatal("postgres db url missing")
	}

	appEnv = strings.TrimSpace(os.Getenv("ENVIRONMENT"))
	if appEnv == "" {
		appEnv = "stg"
	}

	{
		// TODO: remove this
		defaultBucket = strings.TrimSpace(os.Getenv("DEFAULT_BUCKET"))
		if defaultBucket == "" {
			log.Fatal("default bucket not set")
		}
	}
}

// docs.aws.amazon.com/lambda/latest/dg/with-s3-example.html
func handler(cx context.Context, sqsEvent events.SQSEvent) error {
	log.Println("running for env", appEnv)

	ctx, cancel := context.WithTimeout(cx, 12*time.Minute)
	defer cancel()

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("failed to connect to postgres %v", err)
	}

	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to connect to db. error %v", err)
	}

	log.Println("connected to databases")

	shared.MustSetupInstrumenter(&appcfg.Config{Env: appEnv})

	log.Println("processing media events for region", region)

	worker := &mediahose.BatchWorker{
		Idx:         time.Now().UTC().Unix(),
		CloserChan:  make(chan int64),
		ConsumeResp: true,
	}

	lambdaRecords := []*energon.LambdaRecord{}

	for _, msg := range sqsEvent.Records {
		var s3Event events.S3Event

		if !strings.EqualFold(region, msg.AWSRegion) {
			// Don't process media from different region
			continue
		}

		if err := json.Unmarshal([]byte(msg.Body), &s3Event); err != nil {
			log.Printf("failed to parse S3 event from SQS message: %v", err)
			return nil
		}

		log.Printf("sqs msg body %+v", s3Event)

		for id, record := range s3Event.Records {
			wctx, cancel := context.WithCancel(ctx)

			lambdaRecord := &energon.LambdaRecord{
				Bucket:          record.S3.Bucket.Name,
				UploadedPath:    shared.MaybeDecodeURI(record.S3.Object.Key),
				Region:          region,
				RequestedFormat: ".webp",
				BatchID:         fmt.Sprintf("batch_%d", id),
				Ctx:             wctx,
				Cancel:          cancel,
			}

			lambdaRecords = append(lambdaRecords, lambdaRecord)
		}
	}

	batchQueue := make(chan *mediahose.BatchedJob, len(lambdaRecords))
	defer close(batchQueue)

	dispatcher := mediahose.NewDispatcher(100*time.Millisecond, batchQueue, mediahose.NoOpOnComplete)

	log.Println("running the dispatcher for batch job")
	dispatcher.RunInBackground(ctx)

	go worker.Work(ctx, batchQueue)

	dones := []shared.MailBoxCh{}

	encoder := encoders.NewS3Uploader(s3Client)
	imageProcessor := &energon.ImageProcessorHandler{
		S3Client:   s3Client,
		Worker:     worker,
		BatchQueue: batchQueue,
		Encoder:    encoder.Upload,
		Dispatcher: dispatcher,
	}

	pattern := shared.TemplateToRegex("/user_generated/{usr_id}/*")

	for _, record := range lambdaRecords {
		params := shared.ExtractParams(pattern, record.UploadedPath)
		if usrID, ok := params["usr_id"]; ok {
			record.BatchID = usrID
		}

		log.Printf("processing record %+v", record)
		// else, should we ignore instead!
		// BatchKeyHandler, handles how to create a batch

		_dones, err := imageProcessor.Handle(cx, record)
		if err != nil {
			continue
		}

		dones = append(dones, _dones...)
	}

	doneCount := 0

	// TODO:
	//  expect a specific type, which will contain the MediaMetadataInfo
	//  In MetadataService, Use the media_id to group the available sizes
	//  and create the records

	results := []*mediametadata.MediaMetadata{}

	for _, done := range dones {
		res, ok := <-done
		doneCount += 1

		if !ok {
			continue
		}

		result, ok := res.(*mediametadata.MediaMetadata)
		if !ok {
			log.Println("failed to marshal channel data", result)
			continue
		}

		results = append(results, result)
	}

	metasvc := mediametadata.NewMediaMetadaService(
		mediametadata.NewMediaMetadataRepo("postgres", db))

	data := mediametadata.JoinSchema(results)

	fmt.Println("results", shared.Dumps(results))
	log.Println("total results batched", len(results))

	n, err := metasvc.BatchAndCreateMediaMetadata(ctx, data...)
	if err != nil {
		log.Fatal("failed to save records to db")
	}

	mediametadata.UpdateS3Headers(ctx, defaultBucket, s3Client, data...)

	log.Println(map[string]any{
		"record_count":    len(lambdaRecords),
		"enqueued counts": len(dones),
		"done completed":  doneCount,
		"migrated":        n,
	})

	return nil
}

// renames file from `img_Hash.webp` to `img_Hash@3x.webp`
// at this point fileName and ext are both defined
// the validation is ensured at this stage
// func generateAssetNameForSize(fileName, ext, sizeKey string) (string, bool) {
// }

func main() {
	vipsInit.Do(func() {
		vips.Startup(&vips.Config{ConcurrencyLevel: int(50)})
		// defer vips.Shutdown()
	})

	lambda.Start(handler)
}
