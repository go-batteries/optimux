package energon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/mediahose"
	"github.com/go-batteries/optimux/src/shared"
)

// VideoProcessorHandler handles video processing for lambda workers
// Similar to ImageProcessorHandler but for video operations
type VideoProcessorHandler struct {
	S3Client   *s3.Client
	Worker     *mediahose.BatchWorker
	BatchQueue chan *mediahose.BatchedJob
	Encoder    encoders.Encoder
	Dispatcher *mediahose.Dispatcher
}

// Handle processes video upload events and creates jobs for sprite/webvtt/compression
func (vph *VideoProcessorHandler) Handle(ctx context.Context, record *LambdaRecord) ([]shared.MailBoxCh, error) {
	uploadedAsset := filepath.Base(record.UploadedPath)
	ext := filepath.Ext(record.UploadedPath)

	log.Printf("Processing video: %s", uploadedAsset)

	// Skip if not a video file
	if !shared.IsOfMediaType(ext, shared.VideoExtMap) {
		log.Printf("%s is not a video file, skipping", uploadedAsset)
		return nil, errors.New("not_a_video_file")
	}

	// Skip already processed files (avoid infinite loops)
	if strings.Contains(record.UploadedPath, "/processed/") {
		log.Printf("Skipping already processed video: %s", record.UploadedPath)
		return nil, errors.New("already_processed")
	}

	// Extract video ID (filename without extension)
	videoID := strings.TrimSuffix(uploadedAsset, ext)

	// Build video path for processing
	videoPath := filepath.Join(
		fmt.Sprintf("https://%s.s3.%s.amazonaws.com/", record.Bucket, record.Region),
		record.UploadedPath,
	)

	// Extract environment from path (e.g., "stg" or "prod")
	pathParts := strings.Split(record.UploadedPath, "/")
	env := "stg" // default
	if len(pathParts) > 0 {
		env = pathParts[0]
	}

	// Create base processed path
	processedBasePath := fmt.Sprintf("%s/videos/usr_%s/processed/%s", env, record.BatchID, videoID)

	// Define the 3 jobs to create
	jobs := []struct {
		format string
		s3Key  string
		name   string
	}{
		{
			format: "sprites",
			s3Key:  fmt.Sprintf("%s/sprite.webp", processedBasePath),
			name:   "sprite generation",
		},
		{
			format: "webvtt",
			s3Key:  fmt.Sprintf("%s/thumbnails.vtt", processedBasePath),
			name:   "webvtt generation",
		},
		{
			format: "mp4",
			s3Key:  fmt.Sprintf("%s/%s@360p.mp4", processedBasePath, videoID),
			name:   "360p compression",
		},
	}

	dones := []shared.MailBoxCh{}

	for _, jobConfig := range jobs {
		// Check if output already exists in S3
		if vph.checkS3Exists(ctx, record.Bucket, jobConfig.s3Key) {
			log.Printf("Skipping %s - already exists: %s", jobConfig.name, jobConfig.s3Key)
			continue
		}

		log.Printf("Creating job for %s: %s", jobConfig.name, jobConfig.s3Key)

		// Create job
		job := &mediahose.Job{
			ID:              videoID,
			ImagePath:       videoPath, // Reusing ImagePath for video URL
			Format:          jobConfig.format,
			Quality:         80, // Default quality
			Ctx:             ctx,
			CancelCtx:       func() {},
			Done:            make(shared.DoneCh, 1),
			Encoder:         vph.Encoder,
			ErrHandler:      vph.createErrorHandler(jobConfig.name),
			MediaType:       mediahose.MediaTypeVideo,
			OrigPath:        videoPath,
			S3Bucket:        shared.ToPtr(record.Bucket),
			S3Key:           shared.ToPtr(jobConfig.s3Key),
			SkipResize:      false,
			SkipUpload:      false,
			MailBox:         make(shared.MailBoxCh, 1),
			DefaultS3Bucket: record.Bucket,
			Metadata:        make(map[string]interface{}),
		}

		// Add compression-specific metadata for 360p job
		if jobConfig.format == "mp4" {
			job.Metadata["width"] = 640
			job.Metadata["height"] = 360
			job.Metadata["quality"] = 23
		}

		// Add job to dispatcher (batched by user_id)
		vph.Dispatcher.Add(ctx, record.BatchID, job)
		dones = append(dones, job.MailBox)
	}

	log.Printf("🎟️  Queued %d video jobs for %s", len(dones), videoID)
	return dones, nil
}

// checkS3Exists checks if an object exists in S3
func (vph *VideoProcessorHandler) checkS3Exists(ctx context.Context, bucket, key string) bool {
	_, err := vph.S3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: shared.ToPtr(bucket),
		Key:    shared.ToPtr(key),
	})
	return err == nil
}

// createErrorHandler creates an error handler for video processing
func (vph *VideoProcessorHandler) createErrorHandler(jobName string) func(w io.Writer, msg string, code int) {
	return func(w io.Writer, msg string, code int) {
		log.Printf("❌ Video processing failed (%s): %s (code: %d)", jobName, msg, code)
	}
}
