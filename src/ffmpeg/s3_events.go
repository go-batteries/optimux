package ffmpeg

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/go-batteries/optimux/src/mediahose"
)

// S3VideoEvent represents an S3 event for video processing
type S3VideoEvent struct {
	Records []S3VideoRecord `json:"Records"`
}

type S3VideoRecord struct {
	EventName string `json:"eventName"`
	S3        struct {
		Bucket struct {
			Name string `json:"name"`
		} `json:"bucket"`
		Object struct {
			Key  string `json:"key"`
			Size int64  `json:"size"`
		} `json:"object"`
	} `json:"s3"`
}

// VideoFormats supported for processing
var VideoFormats = map[string]bool{
	".mp4":  true,
	".webm": true,
	".avi":  true,
	".mov":  true,
	".mkv":  true,
	".flv":  true,
}

// S3VideoEventHandler processes S3 events for video uploads
type S3VideoEventHandler struct {
	Dispatcher      *mediahose.Dispatcher
	DefaultS3Bucket string
	TempDir         string
}

func NewS3VideoEventHandler(dispatcher *mediahose.Dispatcher, defaultBucket, tempDir string) *S3VideoEventHandler {
	return &S3VideoEventHandler{
		Dispatcher:      dispatcher,
		DefaultS3Bucket: defaultBucket,
		TempDir:         tempDir,
	}
}

// HandleS3Event processes S3 upload events and triggers video processing
func (h *S3VideoEventHandler) HandleS3Event(ctx context.Context, event events.S3Event) error {
	for _, record := range event.Records {
		if err := h.processS3Record(ctx, record); err != nil {
			log.Printf("Failed to process S3 record: %v", err)
			continue
		}
	}
	return nil
}

func (h *S3VideoEventHandler) processS3Record(ctx context.Context, record events.S3EventRecord) error {
	// Check if it's a video file
	if !h.isVideoFile(record.S3.Object.Key) {
		log.Printf("Skipping non-video file: %s", record.S3.Object.Key)
		return nil
	}

	// Skip if it's already a processed file (avoid infinite loops)
	if h.isProcessedFile(record.S3.Object.Key) {
		log.Printf("Skipping processed file: %s", record.S3.Object.Key)
		return nil
	}

	log.Printf("Processing video upload: %s", record.S3.Object.Key)

	// Generate video ID from S3 key
	videoID := h.generateVideoID(record.S3.Object.Key)

	// Create compression job
	compressionJob := h.createVideoJob(videoID, record, "compress")
	h.Dispatcher.Add(ctx, videoID+"_compress", compressionJob)

	// Create sprite generation job
	spriteJob := h.createVideoJob(videoID, record, "generate_sprites")
	h.Dispatcher.Add(ctx, videoID+"_sprites", spriteJob)

	log.Printf("Queued video processing jobs for: %s", videoID)
	return nil
}

func (h *S3VideoEventHandler) isVideoFile(key string) bool {
	ext := strings.ToLower(filepath.Ext(key))
	return VideoFormats[ext]
}

func (h *S3VideoEventHandler) isProcessedFile(key string) bool {
	// Skip files in processed directories
	processedPaths := []string{
		"/compressed/",
		"/sprites/",
		"/frames/",
		"/thumbnails/",
	}
	
	for _, path := range processedPaths {
		if strings.Contains(key, path) {
			return true
		}
	}
	return false
}

func (h *S3VideoEventHandler) generateVideoID(s3Key string) string {
	// Extract filename without extension and path
	filename := filepath.Base(s3Key)
	ext := filepath.Ext(filename)
	return strings.TrimSuffix(filename, ext)
}

func (h *S3VideoEventHandler) createVideoJob(videoID string, record events.S3EventRecord, operation string) *mediahose.Job {
	// Create metadata for video processing
	metadata := map[string]interface{}{
		"operation":     operation,
		"video_id":      videoID,
		"s3_bucket":     record.S3.Bucket.Name,
		"s3_key":        record.S3.Object.Key,
		"file_size":     record.S3.Object.Size,
		"event_name":    record.EventName,
		"temp_dir":      h.TempDir,
	}

	// Set operation-specific configuration
	switch operation {
	case "compress":
		metadata["config"] = map[string]interface{}{
			"quality":    23,
			"preset":     "medium",
			"format":     "mp4",
			"audio_bitrate": "128k",
		}
	case "generate_sprites":
		metadata["config"] = map[string]interface{}{
			"fps":              5,
			"frames_per_sprite": 50,
			"sprite_format":    "webp",
			"tile_layout":      "10x5",
		}
	}

	return &mediahose.Job{
		ID:              fmt.Sprintf("%s_%s_%d", videoID, operation, time.Now().Unix()),
		ImagePath:       record.S3.Object.Key, // Use as video path
		Format:          "mp4",
		MediaType:       mediahose.MediaTypeVideo,
		DefaultS3Bucket: record.S3.Bucket.Name,
		SkipResize:      true,
		SkipUpload:      false,
		Metadata:        metadata,
	}
}

// VideoProcessingConfig defines configuration for different video operations
type VideoProcessingConfig struct {
	Compress struct {
		Quality      int    `yaml:"quality"`
		Preset       string `yaml:"preset"`
		Format       string `yaml:"format"`
		AudioBitrate string `yaml:"audio_bitrate"`
	} `yaml:"compress"`
	
	Sprites struct {
		FPS             int    `yaml:"fps"`
		FramesPerSprite int    `yaml:"frames_per_sprite"`
		Format          string `yaml:"format"`
		TileLayout      string `yaml:"tile_layout"`
	} `yaml:"sprites"`
	
	OnDemand struct {
		MaxFPS      int `yaml:"max_fps"`
		BurstWindow int `yaml:"burst_window_seconds"`
		CacheTTL    int `yaml:"cache_ttl_hours"`
	} `yaml:"on_demand"`
}

// DefaultVideoProcessingConfig returns default configuration
func DefaultVideoProcessingConfig() *VideoProcessingConfig {
	return &VideoProcessingConfig{
		Compress: struct {
			Quality      int    `yaml:"quality"`
			Preset       string `yaml:"preset"`
			Format       string `yaml:"format"`
			AudioBitrate string `yaml:"audio_bitrate"`
		}{
			Quality:      23,
			Preset:       "medium",
			Format:       "mp4",
			AudioBitrate: "128k",
		},
		Sprites: struct {
			FPS             int    `yaml:"fps"`
			FramesPerSprite int    `yaml:"frames_per_sprite"`
			Format          string `yaml:"format"`
			TileLayout      string `yaml:"tile_layout"`
		}{
			FPS:             6,
			FramesPerSprite: 30,
			Format:          "webp",
			TileLayout:      "6x5",
		},
		OnDemand: struct {
			MaxFPS      int `yaml:"max_fps"`
			BurstWindow int `yaml:"burst_window_seconds"`
			CacheTTL    int `yaml:"cache_ttl_hours"`
		}{
			MaxFPS:      30,
			BurstWindow: 1,
			CacheTTL:    24,
		},
	}
}
