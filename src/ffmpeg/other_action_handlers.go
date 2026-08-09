package ffmpeg

import (
	"context"
	"fmt"

	"github.com/roverxio/optimux/src/mediahose"
)

// CompressionActionHandler handles video compression actions
type CompressionActionHandler struct {
	BaseActionHandler
}

func NewCompressionActionHandler() *CompressionActionHandler {
	return &CompressionActionHandler{
		BaseActionHandler: BaseActionHandler{actionName: "compress"},
	}
}

func (cah *CompressionActionHandler) BuildParams(job *mediahose.Job, config *mediahose.ProcessorConfig) map[string]interface{} {
	return buildCommonParams(job, config)
}

func (cah *CompressionActionHandler) HandleResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error {
	return fmt.Errorf("compression result handling not yet implemented")
}

// SegmentationActionHandler handles video segmentation actions
type SegmentationActionHandler struct {
	BaseActionHandler
}

func NewSegmentationActionHandler() *SegmentationActionHandler {
	return &SegmentationActionHandler{
		BaseActionHandler: BaseActionHandler{actionName: "segment"},
	}
}

func (sah *SegmentationActionHandler) BuildParams(job *mediahose.Job, config *mediahose.ProcessorConfig) map[string]interface{} {
	return buildCommonParams(job, config)
}

func (sah *SegmentationActionHandler) HandleResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error {
	return fmt.Errorf("segmentation result handling not yet implemented")
}

// ProbeActionHandler handles video probing actions
type ProbeActionHandler struct {
	BaseActionHandler
}

func NewProbeActionHandler() *ProbeActionHandler {
	return &ProbeActionHandler{
		BaseActionHandler: BaseActionHandler{actionName: "probe_video"},
	}
}

func (pah *ProbeActionHandler) BuildParams(job *mediahose.Job, config *mediahose.ProcessorConfig) map[string]interface{} {
	return buildCommonParams(job, config)
}

func (pah *ProbeActionHandler) HandleResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error {
	return fmt.Errorf("probe result handling not yet implemented")
}

// FrameExtractionActionHandler handles frame extraction actions
type FrameExtractionActionHandler struct {
	BaseActionHandler
}

func NewFrameExtractionActionHandler() *FrameExtractionActionHandler {
	return &FrameExtractionActionHandler{
		BaseActionHandler: BaseActionHandler{actionName: "extract_frames"},
	}
}

func (fah *FrameExtractionActionHandler) BuildParams(job *mediahose.Job, config *mediahose.ProcessorConfig) map[string]interface{} {
	return buildCommonParams(job, config)
}

func (fah *FrameExtractionActionHandler) HandleResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error {
	return fmt.Errorf("frame extraction result handling not yet implemented")
}
