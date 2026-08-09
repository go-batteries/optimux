package ffmpeg

import (
	"context"

	"github.com/roverxio/optimux/src/mediahose"
)

// Factory Integration Layer
// 
// This is the INTEGRATION LAYER that bridges executors with the mediahose JobProcessor system.
// 
// Purpose: Bridges executors with the mediahose JobProcessor system
// 
// What it does:
// - Factory registration - Registers video processors with mediahose
// - ExecutorProcessorWrapper - Wraps executors as JobProcessors
// - Job conversion - Converts mediahose.Job → ExecutionJob
// - Worker pool integration - Makes executors work with existing workers
// 
// Example:
//   // Wraps CommandExecutor so it can be used in worker pool
//   wrapper := NewExecutorProcessorWrapper(executor, "sprites", tempDir)
//   worker.Process(ctx, job) // Uses wrapper internally
// 
// Analogy: This is the ADAPTER 🔌 (makes the engine fit into your system)

// RegisterWithProcessorFactory registers the video processor factory with the mediahose system
func RegisterWithProcessorFactory(configPath string) {
	RegisterVideoProcessorFactoryWithExecutors(configPath)
}

// CreateVideoProcessorForOperation creates a video processor for a specific operation
// This is useful for direct instantiation without going through the factory
func CreateVideoProcessorForOperation(operation, configPath, tempDir string) mediahose.JobProcessor {
	processor := NewVideoJobProcessorWithExecutors(operation, tempDir, configPath)
	
	// You could add operation-specific configuration here if needed
	switch operation {
	case "compress", "compression":
		// Could set specific executor preferences for compression
	case "segment", "segmentation", "scene_breakdown":
		// Could set specific executor preferences for segmentation
	case "extract_frames", "frames":
		// Could set specific executor preferences for frame extraction
	}
	
	return processor
}

// ExecutorProcessorWrapper wraps individual executors as JobProcessors
// This allows using executors directly in the existing worker pool
type ExecutorProcessorWrapper struct {
	executor Executor
	action   string
	tempDir  string
}

// NewExecutorProcessorWrapper creates a wrapper that makes an executor compatible with JobProcessor
func NewExecutorProcessorWrapper(executor Executor, action, tempDir string) *ExecutorProcessorWrapper {
	return &ExecutorProcessorWrapper{
		executor: executor,
		action:   action,
		tempDir:  tempDir,
	}
}

// Process implements mediahose.JobProcessor interface for individual executors
func (epw *ExecutorProcessorWrapper) Process(ctx context.Context, job *mediahose.Job) error {
	// Convert mediahose.Job to ExecutionJob
	executionJob := &ExecutionJob{
		Action:     epw.action,
		InputPath:  job.ImagePath, // Note: ImagePath is used for video path too
		OutputPath: "", // Will be determined by the specific action
		Parameters: make(map[string]interface{}),
	}
	
	// Extract parameters from job metadata
	if job.Metadata != nil {
		for key, value := range job.Metadata {
			executionJob.Parameters[key] = value
		}
	}
	
	// Add processor config parameters
	config := job.GetProcessorConfig()
	if config != nil {
		if config.Quality > 0 {
			executionJob.Parameters["quality"] = config.Quality
		}
		if config.FrameRate > 0 {
			executionJob.Parameters["fps"] = config.FrameRate
		}
		if config.StartTime > 0 {
			executionJob.Parameters["start_time"] = config.StartTime
		}
		if config.Duration > 0 {
			executionJob.Parameters["duration"] = config.Duration
		}
		if config.Boundaries != nil {
			executionJob.Parameters["x"] = config.Boundaries.X
			executionJob.Parameters["y"] = config.Boundaries.Y
			executionJob.Parameters["width"] = config.Boundaries.Width
			executionJob.Parameters["height"] = config.Boundaries.Height
		}
	}
	
	// Execute
	result, err := epw.executor.Execute(ctx, executionJob)
	if err != nil {
		return err
	}
	
	// Handle result based on action type
	// This is a simplified handler - you might want to use the full handlers from VideoJobProcessor
	if len(result.OutputPaths) > 0 {
		// For now, just return success - in a real implementation you'd handle the output appropriately
		return nil
	}
	
	return nil
}

// GetMediaType returns the media type
func (epw *ExecutorProcessorWrapper) GetMediaType() mediahose.MediaType {
	return mediahose.MediaTypeVideo
}
