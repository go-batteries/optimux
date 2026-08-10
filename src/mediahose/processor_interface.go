package mediahose

import (
	"context"
	"log"

	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/shared"
)

// JobProcessor interface for processing different types of media jobs
type JobProcessor interface {
	Process(ctx context.Context, job *Job) error
	GetMediaType() MediaType
}

// ProcessorConfig contains configuration for different processors
type ProcessorConfig struct {
	// Video-specific fields
	Operation   string                 `json:"operation,omitempty"`
	StartTime   float64                `json:"start_time,omitempty"`
	Duration    float64                `json:"duration,omitempty"`
	FrameRate   int                    `json:"frame_rate,omitempty"`
	Boundaries  *FrameBoundaries       `json:"boundaries,omitempty"`
	PageSize    int                    `json:"page_size,omitempty"`
	PageOffset  int                    `json:"page_offset,omitempty"`
	
	// Generic fields
	Quality     int                    `json:"quality,omitempty"`
	Width       int                    `json:"width,omitempty"`
	Height      int                    `json:"height,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// FrameBoundaries defines zoom boundaries for frame extraction
type FrameBoundaries struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Enhanced Job struct to support both image and video processing
func (job *Job) SetProcessorConfig(config *ProcessorConfig) {
	if job.Metadata == nil {
		job.Metadata = make(map[string]interface{})
	}
	job.Metadata["processor_config"] = config
}

func (job *Job) GetProcessorConfig() *ProcessorConfig {
	if job.Metadata == nil {
		return &ProcessorConfig{}
	}
	
	if config, ok := job.Metadata["processor_config"].(*ProcessorConfig); ok {
		return config
	}
	
	return &ProcessorConfig{}
}

// SetProcessor sets the job processor
func (job *Job) SetProcessor(processor JobProcessor) {
	if job.Metadata == nil {
		job.Metadata = make(map[string]interface{})
	}
	job.Metadata["processor"] = processor
}

// GetProcessor gets the job processor
func (job *Job) GetProcessor() JobProcessor {
	if job.Metadata == nil {
		return nil
	}
	
	if processor, ok := job.Metadata["processor"].(JobProcessor); ok {
		return processor
	}
	
	return nil
}

// ProcessorFactory creates appropriate processors for different media types
type ProcessorFactory interface {
	CreateProcessor(mediaType MediaType, operation string, tempDir string) JobProcessor
}

// DefaultProcessorFactory implements ProcessorFactory
type DefaultProcessorFactory struct{}

func NewDefaultProcessorFactory() *DefaultProcessorFactory {
	return &DefaultProcessorFactory{}
}

func (f *DefaultProcessorFactory) CreateProcessor(mediaType MediaType, operation string, tempDir string) JobProcessor {
	switch mediaType {
	case MediaTypeImage:
		return &ImageJobProcessor{
			ImageLoader: nil, // Will be set by the caller
		}
	case MediaTypeVideo:
		// Create video processor dynamically
		return createVideoProcessor(operation, tempDir)
	default:
		return nil
	}
}

// Video processor factory function type
type VideoProcessorFactory func(operation string, tempDir string) JobProcessor

// Global video processor factory
var videoProcessorFactory VideoProcessorFactory

// RegisterVideoProcessorFactory allows video package to register its processor factory
func RegisterVideoProcessorFactory(factory VideoProcessorFactory) {
	log.Println("📹 RegisterVideoProcessorFactory: Registering video processor factory")
	videoProcessorFactory = factory
	log.Printf("✅ Video processor factory registered: %v", videoProcessorFactory != nil)
}

// createVideoProcessor creates a video processor (avoiding circular import)
func createVideoProcessor(operation string, tempDir string) JobProcessor {
	log.Printf("🏭 createVideoProcessor: operation=%s, tempDir=%s, factory=%v", operation, tempDir, videoProcessorFactory != nil)
	if videoProcessorFactory != nil {
		processor := videoProcessorFactory(operation, tempDir)
		log.Printf("✅ Factory created processor: %T", processor)
		return processor
	}
	log.Printf("❌ videoProcessorFactory is nil - not registered!")
	return nil
}

// ImageJobProcessor wraps existing image processing logic
type ImageJobProcessor struct {
	ImageLoader LoadImageStrategy
}

func (ijp *ImageJobProcessor) Process(ctx context.Context, job *Job) error {
	// Use existing image processing logic
	processor := &ImageProcessor{ImageLoader: ijp.ImageLoader}
	
	byts, err := processor.Process(ctx, job)
	if err != nil {
		return err
	}

	if job.MediaType == MediaTypeImage {
		size := job.Sizes[0]
		key := shared.GenerateImageKey(size[0], size[1], job.Quality, job.Format)

		return job.Encoder(ctx, &encoders.ResponseOpts{
			Filename:   key,
			Format:     job.Format,
			Data:       byts,
			S3Key:      job.S3Key,
			S3Bucket:   job.S3Bucket,
			SkipUpload: job.SkipUpload,
		}, job.Resp)
	}

	return nil
}

func (ijp *ImageJobProcessor) GetMediaType() MediaType {
	return MediaTypeImage
}
