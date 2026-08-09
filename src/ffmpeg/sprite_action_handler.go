package ffmpeg

import (
	"context"
	"log"

	"github.com/roverxio/optimux/src/mediahose"
)

// SpriteActionHandler handles sprite generation actions
type SpriteActionHandler struct {
	BaseActionHandler
}

func NewSpriteActionHandler() *SpriteActionHandler {
	return &SpriteActionHandler{
		BaseActionHandler: BaseActionHandler{actionName: "generate_sprites"},
	}
}

// BuildParams builds parameters for sprite generation
func (sah *SpriteActionHandler) BuildParams(job *mediahose.Job, config *mediahose.ProcessorConfig) map[string]interface{} {
	// Start with common params
	params := buildCommonParams(job, config)

	// Add dynamic sprite parameters from job metadata (calculated during sprite generation)
	if job.Metadata != nil {
		if tileWidth, ok := job.Metadata["tile_width"].(int); ok {
			params["tile_width"] = tileWidth
			log.Printf("🎯 SpriteActionHandler: tile_width=%d (dynamic)", tileWidth)
		}
		if tileHeight, ok := job.Metadata["tile_height"].(int); ok {
			params["tile_height"] = tileHeight
			log.Printf("🎯 SpriteActionHandler: tile_height=%d (dynamic)", tileHeight)
		}
		if tileLayout, ok := job.Metadata["tile_layout"].(string); ok {
			params["tile_layout"] = tileLayout
			log.Printf("🎯 SpriteActionHandler: tile_layout=%s (dynamic)", tileLayout)
		}
		if duration, ok := job.Metadata["duration"].(float64); ok {
			params["duration"] = duration
			log.Printf("🎯 SpriteActionHandler: duration=%.1fs (dynamic)", duration)
		}
	}

	return params
}

// HandleResult handles sprite generation results
func (sah *SpriteActionHandler) HandleResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error {
	log.Printf("🎞️  SpriteActionHandler.HandleResult: %d output files", len(result.OutputPaths))

	// Files are already generated in permanent location, no copying needed
	if job.Metadata == nil {
		job.Metadata = make(map[string]interface{})
	}
	job.Metadata["output_paths"] = result.OutputPaths
	log.Printf("💾 Stored %d sprite paths in job metadata", len(result.OutputPaths))

	// Executor doesn't handle HTTP responses - just return success
	// The calling VideoFFmpegProcessor will handle the response
	log.Printf("✅ SpriteActionHandler completed - returning success for caller to handle response")
	return nil
}
