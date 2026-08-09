package ffmpeg

import (
	"context"

	"github.com/roverxio/optimux/src/mediahose"
)

// ActionHandler defines the interface for action-specific handling
// Each action (sprites, webvtt, compress, etc.) implements this interface
type ActionHandler interface {
	// BuildParams builds execution parameters for this action
	BuildParams(job *mediahose.Job, config *mediahose.ProcessorConfig) map[string]interface{}
	
	// HandleResult handles the execution result for this action
	HandleResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error
	
	// GetActionName returns the action name this handler is for
	GetActionName() string
}

// BaseActionHandler provides common functionality for all action handlers
type BaseActionHandler struct {
	actionName string
}

func (bah *BaseActionHandler) GetActionName() string {
	return bah.actionName
}

// buildCommonParams builds common parameters shared by all actions
func buildCommonParams(job *mediahose.Job, config *mediahose.ProcessorConfig) map[string]interface{} {
	params := map[string]interface{}{
		"video_id": job.ID,
	}

	if config.Quality > 0 {
		params["quality"] = config.Quality
	}
	if config.FrameRate > 0 {
		params["fps"] = config.FrameRate
	}
	if config.StartTime > 0 {
		params["start_time"] = config.StartTime
	}
	if config.Duration > 0 {
		params["duration"] = config.Duration
	}
	if config.Boundaries != nil {
		params["x"] = config.Boundaries.X
		params["y"] = config.Boundaries.Y
		params["width"] = config.Boundaries.Width
		params["height"] = config.Boundaries.Height
	}

	return params
}
