package ffmpeg

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-batteries/optimux/src/shared"
)

// FFmpegProcessor interface for different video processing operations
type FFmpegProcessor interface {
	Process(ctx context.Context, config *ProcessingConfig) (*ProcessingResult, error)
}

// ProcessingConfig contains configuration for video processing
type ProcessingConfig struct {
	InputPath    string
	OutputPath   string
	VideoID      string
	Operation    ProcessingOperation
	StartTime    float64 // in seconds
	Duration     float64 // in seconds
	FrameRate    int
	Quality      int
	Width        int
	Height       int
	Format       string
	Boundaries   *FrameBoundaries
	PageSize     int     // Number of frames to extract (for pagination)
	PageOffset   int     // Starting frame index (for pagination)
}

// ProcessingOperation defines the type of processing
type ProcessingOperation string

const (
	OperationCompress      ProcessingOperation = "compress"
	OperationSegment       ProcessingOperation = "segment"
	OperationExtractFrames ProcessingOperation = "extract_frames"
)

// FrameBoundaries defines zoom boundaries for frame extraction
type FrameBoundaries struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ProcessingResult contains the result of video processing
type ProcessingResult struct {
	OutputPaths []string
	Duration    time.Duration
	FrameCount  int
	Metadata    map[string]interface{}
}

// VideoCompressionProcessor handles video compression
type VideoCompressionProcessor struct {
	TempDir string
}

func NewVideoCompressionProcessor(tempDir string) *VideoCompressionProcessor {
	return &VideoCompressionProcessor{
		TempDir: tempDir,
	}
}

func (vcp *VideoCompressionProcessor) Process(ctx context.Context, config *ProcessingConfig) (*ProcessingResult, error) {
	defer shared.Bench(fmt.Sprintf("VideoCompressionProcessor.Process %s", config.VideoID))()

	outputPath := filepath.Join(vcp.TempDir, fmt.Sprintf("%s_compressed.mp4", config.VideoID))
	
	// Build ffmpeg command for compression
	args := []string{
		"-i", config.InputPath,
		"-c:v", "libx264",
		"-crf", strconv.Itoa(config.Quality),
		"-preset", "fast",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y", // overwrite output file
		outputPath,
	}

	if config.Width > 0 && config.Height > 0 {
		args = append(args[:len(args)-1], "-vf", fmt.Sprintf("scale=%d:%d", config.Width, config.Height), args[len(args)-1])
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	
	start := time.Now()
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg compression failed: %w", err)
	}

	return &ProcessingResult{
		OutputPaths: []string{outputPath},
		Duration:    time.Since(start),
		Metadata: map[string]interface{}{
			"operation": "compress",
			"quality":   config.Quality,
		},
	}, nil
}

// VideoSegmentProcessor handles video segmentation into 1-second chunks
type VideoSegmentProcessor struct {
	TempDir string
}

func NewVideoSegmentProcessor(tempDir string) *VideoSegmentProcessor {
	return &VideoSegmentProcessor{
		TempDir: tempDir,
	}
}

func (vsp *VideoSegmentProcessor) Process(ctx context.Context, config *ProcessingConfig) (*ProcessingResult, error) {
	defer shared.Bench(fmt.Sprintf("VideoSegmentProcessor.Process %s", config.VideoID))()

	// Get video duration first
	duration, err := vsp.getVideoDuration(ctx, config.InputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get video duration: %w", err)
	}

	var outputPaths []string
	segmentDuration := 1.0 // 1 second segments

	for start := 0.0; start < duration; start += segmentDuration {
		segmentPath := filepath.Join(vsp.TempDir, fmt.Sprintf("%s_segment_%03d.mp4", config.VideoID, int(start)))
		
		args := []string{
			"-i", config.InputPath,
			"-ss", fmt.Sprintf("%.2f", start),
			"-t", fmt.Sprintf("%.2f", segmentDuration),
			"-c", "copy",
			"-avoid_negative_ts", "make_zero",
			"-y",
			segmentPath,
		}

		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("ffmpeg segmentation failed at %.2fs: %w", start, err)
		}

		outputPaths = append(outputPaths, segmentPath)
	}

	return &ProcessingResult{
		OutputPaths: outputPaths,
		Duration:    time.Since(time.Now()),
		Metadata: map[string]interface{}{
			"operation":        "segment",
			"segment_duration": segmentDuration,
			"total_segments":   len(outputPaths),
		},
	}, nil
}

// SpriteSheetProcessor generates sprite sheets (tiled thumbnails)
type SpriteSheetProcessor struct {
	TempDir string
}

func NewSpriteSheetProcessor(tempDir string) *SpriteSheetProcessor {
	return &SpriteSheetProcessor{
		TempDir: tempDir,
	}
}

func (ssp *SpriteSheetProcessor) Process(ctx context.Context, config *ProcessingConfig) (*ProcessingResult, error) {
	defer shared.Bench(fmt.Sprintf("SpriteSheetProcessor.Process %s", config.VideoID))()

	// Use config values or defaults
	frameRate := config.FrameRate
	if frameRate <= 0 {
		frameRate = 5 // default 5fps for sprites
	}

	// Thumbnail dimensions (can be made configurable later)
	thumbWidth := config.Width
	thumbHeight := config.Height
	if thumbWidth <= 0 {
		thumbWidth = 160 // default thumbnail width
	}
	if thumbHeight <= 0 {
		thumbHeight = 90 // default thumbnail height
	}

	// Tile layout (can be made configurable later)
	columns := 10
	rows := 10

	// Create output directory
	outputDir := filepath.Join(ssp.TempDir, fmt.Sprintf("%s_sprites", config.VideoID))
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Output sprite sheet path
	format := config.Format
	if format == "" {
		format = "jpg"
	}
	outputPath := filepath.Join(outputDir, fmt.Sprintf("sprite_sheet.%s", format))

	// FFmpeg command to generate sprite sheet with tile filter
	args := []string{
		"-i", config.InputPath,
		"-vf", fmt.Sprintf("fps=%d,scale=%d:%d,tile=%dx%d", frameRate, thumbWidth, thumbHeight, columns, rows),
		"-an", // disable audio
		"-q:v", fmt.Sprintf("%d", config.Quality/10), // convert quality (0-100) to qscale (1-10)
		"-y",
		outputPath,
	}

	log.Printf("🎬 FFmpeg sprite command: ffmpeg %v", args)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("❌ FFmpeg error: %s", string(output))
		return nil, fmt.Errorf("ffmpeg sprite generation failed: %w", err)
	}

	log.Printf("✅ Sprite sheet generated: %s", outputPath)

	return &ProcessingResult{
		OutputPaths: []string{outputPath},
		FrameCount:  columns * rows, // Max frames in sprite sheet
		Metadata: map[string]interface{}{
			"sprite_width":  thumbWidth * columns,
			"sprite_height": thumbHeight * rows,
			"tile_width":    thumbWidth,
			"tile_height":   thumbHeight,
			"columns":       columns,
			"rows":          rows,
			"fps":           frameRate,
		},
	}, nil
}

// FrameExtractionProcessor handles frame extraction with pagination
type FrameExtractionProcessor struct {
	TempDir string
}

func NewFrameExtractionProcessor(tempDir string) *FrameExtractionProcessor {
	return &FrameExtractionProcessor{
		TempDir: tempDir,
	}
}

func (fep *FrameExtractionProcessor) Process(ctx context.Context, config *ProcessingConfig) (*ProcessingResult, error) {
	defer shared.Bench(fmt.Sprintf("FrameExtractionProcessor.Process %s", config.VideoID))()

	frameRate := config.FrameRate
	if frameRate <= 0 {
		frameRate = 13 // default to 13fps as specified
	}

	// Check if pagination is requested
	pageSize := config.PageSize
	pageOffset := config.PageOffset
	usePagination := pageSize > 0

	// Create output directory for frames
	frameDir := filepath.Join(fep.TempDir, fmt.Sprintf("%s_frames", config.VideoID))
	if err := os.MkdirAll(frameDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create frame directory: %w", err)
	}

	var adjustedStartTime float64
	var adjustedDuration float64
	var frameNumberOffset int

	if usePagination {
		// OPTIMIZED: Only extract the frames we need for this page
		// Calculate which frames to extract based on pagination
		frameNumberOffset = pageOffset
		adjustedStartTime = config.StartTime + (float64(pageOffset) / float64(frameRate))
		adjustedDuration = float64(pageSize) / float64(frameRate)
		
		// Ensure we don't exceed the original duration
		if config.Duration > 0 {
			maxEndTime := config.StartTime + config.Duration
			calculatedEndTime := adjustedStartTime + adjustedDuration
			if calculatedEndTime > maxEndTime {
				adjustedDuration = maxEndTime - adjustedStartTime
			}
		}
	} else {
		// Extract all frames (original behavior)
		adjustedStartTime = config.StartTime
		adjustedDuration = config.Duration
		frameNumberOffset = 0
	}

	args := []string{
		"-i", config.InputPath,
		"-ss", fmt.Sprintf("%.3f", adjustedStartTime),
	}

	if adjustedDuration > 0 {
		args = append(args, "-t", fmt.Sprintf("%.3f", adjustedDuration))
	}

	// Add frame rate and format options
	args = append(args,
		"-vf", fmt.Sprintf("fps=%d", frameRate),
		"-q:v", "2", // high quality JPEG
	)

	// Add crop filter if boundaries are specified
	if config.Boundaries != nil {
		cropFilter := fmt.Sprintf("crop=%d:%d:%d:%d", 
			config.Boundaries.Width, config.Boundaries.Height,
			config.Boundaries.X, config.Boundaries.Y)
		vfFilter := fmt.Sprintf("fps=%d,%s", frameRate, cropFilter)
		args = append(args[:len(args)-2], "-vf", vfFilter)
		args = append(args, args[len(args)-2:]...)
	}

	outputPattern := filepath.Join(frameDir, "frame_%04d.jpg")
	args = append(args, "-y", outputPattern)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	
	start := time.Now()
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg frame extraction failed: %w", err)
	}

	// Collect generated frame paths
	files, err := filepath.Glob(filepath.Join(frameDir, "frame_*.jpg"))
	if err != nil {
		return nil, fmt.Errorf("failed to list generated frames: %w", err)
	}

	return &ProcessingResult{
		OutputPaths: files,
		Duration:    time.Since(start),
		FrameCount:  len(files),
		Metadata: map[string]interface{}{
			"operation":        "extract_frames",
			"frame_rate":       frameRate,
			"start_time":       config.StartTime,
			"duration":         config.Duration,
			"boundaries":       config.Boundaries,
			"paginated":        usePagination,
			"page_offset":      pageOffset,
			"page_size":        pageSize,
			"frame_offset":     frameNumberOffset,
			"extracted_start":  adjustedStartTime,
			"extracted_duration": adjustedDuration,
		},
	}, nil
}

// Helper method to get video duration
func (vsp *VideoSegmentProcessor) getVideoDuration(ctx context.Context, inputPath string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", 
		"-v", "quiet",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		inputPath)
	
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w", err)
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %w", err)
	}

	return duration, nil
}
