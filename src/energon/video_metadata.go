package energon

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// VideoProcessingResult contains the results of video processing
type VideoProcessingResult struct {
	VideoID       string
	UserID        string
	Env           string
	SpritePath    string
	WebVTTPath    string
	CompressedPath string
	ProcessedAt   time.Time
}

// UpdateVideoS3Metadata updates the original video's S3 metadata with processing results
func UpdateVideoS3Metadata(ctx context.Context, s3Client *s3.Client, bucket, videoKey string, result *VideoProcessingResult) error {
	log.Printf("Updating S3 metadata for video: %s", videoKey)

	// Build metadata map
	metadata := map[string]string{
		"processed-at": result.ProcessedAt.Format(time.RFC3339),
	}

	if result.SpritePath != "" {
		metadata["sprites-path"] = result.SpritePath
	}

	if result.WebVTTPath != "" {
		metadata["webvtt-path"] = result.WebVTTPath
	}

	if result.CompressedPath != "" {
		metadata["compressed-360p"] = result.CompressedPath
	}

	// Copy object to itself with new metadata (S3 metadata update pattern)
	copySource := fmt.Sprintf("%s/%s", bucket, videoKey)
	
	_, err := s3Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String(videoKey),
		CopySource:        aws.String(copySource),
		Metadata:          metadata,
		MetadataDirective: types.MetadataDirectiveReplace,
	})

	if err != nil {
		log.Printf("⚠️  Failed to update S3 metadata for %s: %v", videoKey, err)
		return err
	}

	log.Printf("✅ Successfully updated S3 metadata for %s", videoKey)
	return nil
}

// BuildVideoProcessingResult builds a result from processed job outputs
func BuildVideoProcessingResult(videoID, userID, env string, processedPaths map[string]string) *VideoProcessingResult {
	result := &VideoProcessingResult{
		VideoID:     videoID,
		UserID:      userID,
		Env:         env,
		ProcessedAt: time.Now().UTC(),
	}

	// Map job formats to result fields
	if path, ok := processedPaths["sprites"]; ok {
		result.SpritePath = path
	}

	if path, ok := processedPaths["webvtt"]; ok {
		result.WebVTTPath = path
	}

	if path, ok := processedPaths["mp4"]; ok {
		result.CompressedPath = path
	}

	return result
}
