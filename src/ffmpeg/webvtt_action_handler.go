package ffmpeg

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/roverxio/optimux/src/mediahose"
	"github.com/roverxio/optimux/src/shared"
)

// WebVTTActionHandler handles WebVTT generation actions
type WebVTTActionHandler struct {
	BaseActionHandler
}

func NewWebVTTActionHandler() *WebVTTActionHandler {
	return &WebVTTActionHandler{
		BaseActionHandler: BaseActionHandler{actionName: "generate_webvtt"},
	}
}

// BuildParams builds parameters for WebVTT generation
func (wah *WebVTTActionHandler) BuildParams(job *mediahose.Job, config *mediahose.ProcessorConfig) map[string]interface{} {
	// WebVTT generation doesn't need FFmpeg parameters
	// It's generated programmatically from sprite metadata
	return buildCommonParams(job, config)
}

// HandleResult handles WebVTT generation results
func (wah *WebVTTActionHandler) HandleResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error {
	log.Printf("📝 WebVTTActionHandler.HandleResult: Starting integrated ffprobe + WebVTT generation")

	// Step 1: Get video metadata using ffprobe
	probeData, err := wah.runFFProbe(ctx, job)
	if err != nil {
		return fmt.Errorf("failed to probe video for WebVTT generation: %w", err)
	}

	// Step 2: Extract video properties
	videoProps, err := wah.extractVideoProperties(probeData)
	if err != nil {
		return fmt.Errorf("failed to extract video properties: %w", err)
	}

	log.Printf("🎬 Video properties: duration=%.2fs, fps=%.2f, frames=%d",
		videoProps.Duration, videoProps.FPS, videoProps.TotalFrames)

	// Step 3: Calculate sprite frame positions using job metadata
	frames := wah.calculateSpriteFrames(job, videoProps)
	log.Printf("🎞️  Calculated %d sprite frames", len(frames))

	// Step 4: Generate WebVTT content
	webvttContent, err := wah.generateDynamicWebVTT(job.ID, frames)
	if err != nil {
		return fmt.Errorf("failed to generate dynamic WebVTT: %w", err)
	}

	log.Printf("📝 Generated dynamic WebVTT: %d bytes", len(webvttContent))

	// Store in job metadata
	if job.Metadata == nil {
		job.Metadata = make(map[string]interface{})
	}
	job.Metadata["webvtt_content"] = webvttContent
	job.Metadata["video_properties"] = videoProps
	job.Metadata["sprite_frames"] = frames

	// Return WebVTT content directly if there's a response writer
	if job.Resp != nil {
		job.Resp.Header().Set("Content-Type", "text/vtt")
		job.Resp.Write([]byte(webvttContent))
		log.Printf("✅ WebVTT content written to response")
	}

	return nil
}

// runFFProbe executes ffprobe to get video metadata
func (wah *WebVTTActionHandler) runFFProbe(ctx context.Context, job *mediahose.Job) (map[string]interface{}, error) {
	log.Printf("🔍 Running ffprobe for video: %s", job.ImagePath)

	// Use shared utility functions
	duration, err := shared.GetVideoDuration(ctx, job.ImagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get duration: %w", err)
	}

	width, height, err := shared.GetVideoDimensions(ctx, job.ImagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get dimensions: %w", err)
	}

	probeData := map[string]interface{}{
		"duration": duration,
		"width":    width,
		"height":   height,
	}

	log.Printf("📊 FFProbe results: duration=%.2fs, dimensions=%dx%d", duration, width, height)
	return probeData, nil
}

// extractVideoProperties parses ffprobe output to extract video properties
func (wah *WebVTTActionHandler) extractVideoProperties(probeData map[string]interface{}) (*VideoProperties, error) {
	duration, _ := probeData["duration"].(float64)
	width, _ := probeData["width"].(int)
	height, _ := probeData["height"].(int)

	// Assume 5fps for sprite generation
	fps := 5.0
	totalFrames := int(duration * fps)

	return &VideoProperties{
		Duration:    duration,
		FPS:         fps,
		TotalFrames: totalFrames,
		Width:       width,
		Height:      height,
	}, nil
}

// calculateSpriteFrames calculates frame positions in sprite sheet using job metadata
func (wah *WebVTTActionHandler) calculateSpriteFrames(job *mediahose.Job, props *VideoProperties) []SpriteFrame {
	// Get dynamic values from job metadata (set during sprite generation)
	tileWidth, _ := job.Metadata["tile_width"].(int)
	tileHeight, _ := job.Metadata["tile_height"].(int)
	gridSize, _ := job.Metadata["grid_size"].(int)
	fps, _ := job.Metadata["fps"].(int)
	duration, _ := job.Metadata["duration"].(float64)

	// Fallback to defaults if metadata not available
	if tileWidth == 0 || tileHeight == 0 || gridSize == 0 {
		log.Printf("⚠️  Missing sprite metadata, using defaults")
		tileWidth = 160
		tileHeight = 90
		gridSize = 10
		fps = 5
		duration = props.Duration
	}

	log.Printf("🎯 SPRITE CONFIG (Dynamic): %dx%d grid, %dx%d tiles, %dfps",
		gridSize, gridSize, tileWidth, tileHeight, fps)

	frameInterval := 1.0 / float64(fps) // Time between sprite frames

	var frames []SpriteFrame
	totalFrames := int(duration * float64(fps))

	log.Printf("📊 FRAME CALCULATION: duration=%.2fs, fps=%d → %d frames",
		duration, fps, totalFrames)

	for i := 0; i < totalFrames; i++ {
		startTime := float64(i) * frameInterval
		endTime := startTime + frameInterval

		// Calculate position in sprite grid (square grid)
		row := i / gridSize
		col := i % gridSize

		x := col * tileWidth
		y := row * tileHeight

		frames = append(frames, SpriteFrame{
			StartTime: wah.formatTime(startTime),
			EndTime:   wah.formatTime(endTime),
			X:         x,
			Y:         y,
			Width:     tileWidth,
			Height:    tileHeight,
		})
	}

	log.Printf("✅ Generated %d WebVTT cues", len(frames))
	return frames
}

// formatTime formats seconds to WebVTT time format (HH:MM:SS.mmm)
func (wah *WebVTTActionHandler) formatTime(seconds float64) string {
	hours := int(seconds) / 3600
	minutes := (int(seconds) % 3600) / 60
	secs := int(seconds) % 60
	millis := int((seconds - float64(int(seconds))) * 1000)

	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, secs, millis)
}

// generateDynamicWebVTT generates WebVTT content with calculated frame data
func (wah *WebVTTActionHandler) generateDynamicWebVTT(videoID string, frames []SpriteFrame) (string, error) {
	var webvtt strings.Builder

	webvtt.WriteString("WEBVTT\n\n")

	// Use the same sprite URL format as the sprite generation
	spriteURL := fmt.Sprintf("/sprites/%s.webp", videoID)
	log.Printf("🔗 SPRITE URL: %s", spriteURL)

	for _, frame := range frames {
		webvtt.WriteString(fmt.Sprintf("%s --> %s\n", frame.StartTime, frame.EndTime))
		webvtt.WriteString(fmt.Sprintf("%s#xywh=%d,%d,%d,%d\n\n",
			spriteURL, frame.X, frame.Y, frame.Width, frame.Height))
	}

	// Trim trailing newlines
	content := strings.TrimRight(webvtt.String(), "\n")
	return content, nil
}
