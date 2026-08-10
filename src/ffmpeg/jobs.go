package ffmpeg

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/shared"
)

// VideoJob represents a video processing task following the existing Job pattern
type VideoJob struct {
	ID              string
	VideoPath       string
	Operation       ProcessingOperation
	Config          *ProcessingConfig
	Resp            http.ResponseWriter
	Done            shared.DoneCh
	MailBox         shared.MailBoxCh
	Ctx             context.Context
	CancelCtx       context.CancelFunc
	Encoder         encoders.Encoder
	ErrHandler      func(w http.ResponseWriter, msg string, code int)
	VideoLoader     VideoLoadStrategy
	OrigPath        string
	DefaultS3Bucket string

	S3Bucket   *string
	S3Key      *string
	SkipUpload bool

	// Video-specific fields
	StartTime    float64
	Duration     float64
	FrameRate    int
	Quality      int
	Boundaries   *FrameBoundaries
	PageSize     int
	PageOffset   int
}

// VideoLoadStrategy interface for loading videos from different sources
type VideoLoadStrategy interface {
	LoadVideo(job *VideoJob) (io.ReadCloser, error)
}

// S3VideoLoader loads videos from S3
type S3VideoLoader struct {
	Client S3VideoClient
}

type S3VideoClient interface {
	DownloadVideo(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}

func (s3l *S3VideoLoader) LoadVideo(job *VideoJob) (io.ReadCloser, error) {
	if job.S3Bucket == nil || job.S3Key == nil {
		return nil, fmt.Errorf("S3 bucket or key not specified")
	}
	return s3l.Client.DownloadVideo(job.Ctx, *job.S3Bucket, *job.S3Key)
}

// HTTPVideoLoader loads videos from HTTP URLs
type HTTPVideoLoader struct {
	Client HTTPVideoClient
}

type HTTPVideoClient interface {
	DownloadVideo(ctx context.Context, url string) (io.ReadCloser, error)
}

func (hl *HTTPVideoLoader) LoadVideo(job *VideoJob) (io.ReadCloser, error) {
	return hl.Client.DownloadVideo(job.Ctx, job.VideoPath)
}

// Legacy video job structure - will be replaced by polymorphic mediahose.Job
type VideoJobProcessorOld struct {
	Processor   FFmpegProcessor
	TempDir     string
	VideoLoader VideoLoadStrategy
}

func NewVideoJobProcessorOld(processor FFmpegProcessor, tempDir string, loader VideoLoadStrategy) *VideoJobProcessorOld {
	return &VideoJobProcessorOld{
		Processor:   processor,
		TempDir:     tempDir,
		VideoLoader: loader,
	}
}

func (vjp *VideoJobProcessorOld) Process(ctx context.Context, job *VideoJob) error {
	// Download video to temporary location
	videoReader, err := vjp.VideoLoader.LoadVideo(job)
	if err != nil {
		return fmt.Errorf("failed to load video: %w", err)
	}
	defer videoReader.Close()

	// Save to temporary file
	tempVideoPath := filepath.Join(vjp.TempDir, fmt.Sprintf("%s_input.mp4", job.ID))
	tempFile, err := os.Create(tempVideoPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempVideoPath)
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, videoReader); err != nil {
		return fmt.Errorf("failed to save video to temp file: %w", err)
	}
	tempFile.Close()

	// Configure processing
	job.Config.InputPath = tempVideoPath
	job.Config.VideoID = job.ID
	job.Config.Operation = job.Operation
	job.Config.StartTime = job.StartTime
	job.Config.Duration = job.Duration
	job.Config.FrameRate = job.FrameRate
	job.Config.Quality = job.Quality
	job.Config.Boundaries = job.Boundaries

	// Process video
	result, err := vjp.Processor.Process(ctx, job.Config)
	if err != nil {
		return fmt.Errorf("video processing failed: %w", err)
	}

	// Handle different output types based on operation
	switch job.Operation {
	case OperationCompress:
		return vjp.handleCompressionResult(ctx, job, result)
	case OperationSegment:
		return vjp.handleSegmentationResult(ctx, job, result)
	case OperationExtractFrames:
		return vjp.handleFrameExtractionResult(ctx, job, result)
	default:
		return fmt.Errorf("unsupported operation: %s", job.Operation)
	}
}

func (vjp *VideoJobProcessorOld) handleCompressionResult(ctx context.Context, job *VideoJob, result *ProcessingResult) error {
	if len(result.OutputPaths) == 0 {
		return fmt.Errorf("no output files generated")
	}

	// Read compressed video
	compressedPath := result.OutputPaths[0]
	defer os.Remove(compressedPath)

	data, err := os.ReadFile(compressedPath)
	if err != nil {
		return fmt.Errorf("failed to read compressed video: %w", err)
	}

	// Upload using encoder
	return job.Encoder(ctx, &encoders.ResponseOpts{
		Filename:   fmt.Sprintf("%s_compressed.mp4", job.ID),
		Format:     ".mp4",
		Data:       data,
		S3Bucket:   job.S3Bucket,
		S3Key:      job.S3Key,
		SkipUpload: job.SkipUpload,
	}, job.Resp)
}

func (vjp *VideoJobProcessorOld) handleSegmentationResult(ctx context.Context, job *VideoJob, result *ProcessingResult) error {
	// Create a ZIP archive of all segments or upload individually
	segments := make([]map[string]interface{}, 0, len(result.OutputPaths))
	
	for i, segmentPath := range result.OutputPaths {
		defer os.Remove(segmentPath)
		
		data, err := os.ReadFile(segmentPath)
		if err != nil {
			continue // Skip failed segments
		}

		segmentKey := fmt.Sprintf("%s_segment_%03d.mp4", job.ID, i)
		
		if !job.SkipUpload {
			err = job.Encoder(ctx, &encoders.ResponseOpts{
				Filename: segmentKey,
				Format:   ".mp4",
				Data:     data,
				S3Bucket: job.S3Bucket,
				S3Key:    shared.ToPtr(segmentKey),
			}, nil) // No HTTP response for individual segments
			
			if err != nil {
				continue
			}
		}

		segments = append(segments, map[string]interface{}{
			"segment_id": i,
			"filename":   segmentKey,
			"duration":   1.0,
			"size":       len(data),
		})
	}

	// Return segment metadata as JSON response
	response := map[string]interface{}{
		"video_id":      job.ID,
		"total_segments": len(segments),
		"segments":      segments,
		"metadata":      result.Metadata,
	}

	return shared.WriteJSONResponse(job.Resp, response)
}

func (vjp *VideoJobProcessorOld) handleFrameExtractionResult(ctx context.Context, job *VideoJob, result *ProcessingResult) error {
	frames := make([]map[string]interface{}, 0, len(result.OutputPaths))
	
	// Handle pagination
	startIdx := job.PageOffset
	endIdx := startIdx + job.PageSize
	if endIdx > len(result.OutputPaths) {
		endIdx = len(result.OutputPaths)
	}
	if startIdx >= len(result.OutputPaths) {
		startIdx = 0
		endIdx = 0
	}

	for i := startIdx; i < endIdx; i++ {
		framePath := result.OutputPaths[i]
		defer os.Remove(framePath)
		
		data, err := os.ReadFile(framePath)
		if err != nil {
			continue
		}

		frameKey := fmt.Sprintf("%s_frame_%04d.jpg", job.ID, i)
		
		if !job.SkipUpload {
			err = job.Encoder(ctx, &encoders.ResponseOpts{
				Filename: frameKey,
				Format:   ".jpg",
				Data:     data,
				S3Bucket: job.S3Bucket,
				S3Key:    shared.ToPtr(frameKey),
			}, nil)
			
			if err != nil {
				continue
			}
		}

		frames = append(frames, map[string]interface{}{
			"frame_id":  i,
			"filename":  frameKey,
			"timestamp": float64(i) / float64(job.FrameRate),
			"size":      len(data),
		})
	}

	// Return paginated frame metadata
	response := map[string]interface{}{
		"video_id":     job.ID,
		"total_frames": result.FrameCount,
		"page_offset":  startIdx,
		"page_size":    len(frames),
		"frames":       frames,
		"metadata":     result.Metadata,
		"has_more":     endIdx < len(result.OutputPaths),
	}

	return shared.WriteJSONResponse(job.Resp, response)
}
