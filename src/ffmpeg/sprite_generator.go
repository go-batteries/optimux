package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-batteries/optimux/src/shared"
)

// SpriteGenerator handles creation of video spritesheets
type SpriteGenerator struct {
	TempDir string
}

// SpriteManifest contains metadata for sprite navigation
type SpriteManifest struct {
	VideoID         string          `json:"video_id"`
	Duration        float64         `json:"duration_seconds"`
	FPS             int             `json:"fps"`
	FramesPerSprite int             `json:"frames_per_sprite"`
	SpriteFormat    string          `json:"sprite_format"`
	TileLayout      string          `json:"tile_layout"`
	Sprites         []SpriteInfo    `json:"sprites"`
	CreatedAt       time.Time       `json:"created_at"`
}

// SpriteInfo contains information about individual sprite files
type SpriteInfo struct {
	Index      int     `json:"index"`
	Filename   string  `json:"filename"`
	StartTime  float64 `json:"start_time"`
	EndTime    float64 `json:"end_time"`
	FrameCount int     `json:"frame_count"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
}

func NewSpriteGenerator(tempDir string) *SpriteGenerator {
	return &SpriteGenerator{
		TempDir: tempDir,
	}
}

// GenerateSprites creates spritesheets from video with manifest
func (sg *SpriteGenerator) GenerateSprites(ctx context.Context, config *ProcessingConfig) (*ProcessingResult, error) {
	defer shared.Bench(fmt.Sprintf("SpriteGenerator.GenerateSprites %s", config.VideoID))()

	// Get video duration and dimensions
	duration, err := sg.GetVideoDuration(ctx, config.InputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get video duration: %w", err)
	}

	width, height, err := sg.GetVideoDimensions(ctx, config.InputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get video dimensions: %w", err)
	}

	// Extract configuration from metadata (use defaults for now)
	fps := 5
	framesPerSprite := 50
	spriteFormat := "webp"
	tileLayout := "10x5"

	// Calculate sprite parameters
	totalFrames := int(duration * float64(fps))
	spriteCount := (totalFrames + framesPerSprite - 1) / framesPerSprite

	// Create working directory
	workDir := filepath.Join(sg.TempDir, fmt.Sprintf("sprites_%s", config.VideoID))
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	var sprites []SpriteInfo
	var outputPaths []string

	// Generate each sprite
	for i := 0; i < spriteCount; i++ {
		startTime := float64(i*framesPerSprite) / float64(fps)
		endTime := float64((i+1)*framesPerSprite) / float64(fps)
		if endTime > duration {
			endTime = duration
		}

		spriteInfo, spritePath, err := sg.generateSingleSprite(ctx, config, i, startTime, endTime, fps, framesPerSprite, tileLayout, spriteFormat, workDir, width, height)
		if err != nil {
			return nil, fmt.Errorf("failed to generate sprite %d: %w", i, err)
		}

		sprites = append(sprites, *spriteInfo)
		outputPaths = append(outputPaths, spritePath)
	}

	// Create manifest
	manifest := &SpriteManifest{
		VideoID:         config.VideoID,
		Duration:        duration,
		FPS:             fps,
		FramesPerSprite: framesPerSprite,
		SpriteFormat:    spriteFormat,
		TileLayout:      tileLayout,
		Sprites:         sprites,
		CreatedAt:       time.Now(),
	}

	// Save manifest
	manifestPath := filepath.Join(workDir, "manifest.json")
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	outputPaths = append(outputPaths, manifestPath)

	return &ProcessingResult{
		OutputPaths: outputPaths,
		Metadata: map[string]interface{}{
			"video_id":          config.VideoID,
			"operation":         "generate_sprites",
			"sprite_count":      len(sprites),
			"total_frames":      totalFrames,
			"duration":          duration,
			"fps":               fps,
			"frames_per_sprite": framesPerSprite,
			"manifest_path":     manifestPath,
		},
	}, nil
}

func (sg *SpriteGenerator) generateSingleSprite(ctx context.Context, config *ProcessingConfig, index int, startTime, endTime float64, fps, framesPerSprite int, tileLayout, format string, workDir string, videoWidth, videoHeight int) (*SpriteInfo, string, error) {
	// Extract frames for this sprite
	frameDir := filepath.Join(workDir, fmt.Sprintf("frames_%d", index))
	if err := os.MkdirAll(frameDir, 0755); err != nil {
		return nil, "", fmt.Errorf("failed to create frame directory: %w", err)
	}

	// Extract frames using ffmpeg
	framePattern := filepath.Join(frameDir, "frame_%05d.jpg")
	duration := endTime - startTime

	args := []string{
		"-i", config.InputPath,
		"-ss", fmt.Sprintf("%.3f", startTime),
		"-t", fmt.Sprintf("%.3f", duration),
		"-vf", fmt.Sprintf("fps=%d", fps),
		"-q:v", "2", // High quality for frames
		"-y",
		framePattern,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("failed to extract frames: %w", err)
	}

	// Count actual frames extracted
	frameFiles, err := filepath.Glob(filepath.Join(frameDir, "frame_*.jpg"))
	if err != nil {
		return nil, "", fmt.Errorf("failed to count frames: %w", err)
	}

	actualFrameCount := len(frameFiles)
	if actualFrameCount == 0 {
		return nil, "", fmt.Errorf("no frames extracted")
	}

	// Create sprite using montage
	spritePath := filepath.Join(workDir, fmt.Sprintf("sprite_%03d.%s", index, format))
	
	// Parse tile layout (e.g., "6x5")
	tileParts := strings.Split(tileLayout, "x")
	if len(tileParts) != 2 {
		return nil, "", fmt.Errorf("invalid tile layout: %s", tileLayout)
	}

	montageArgs := []string{
		filepath.Join(frameDir, "frame_*.jpg"),
		"-tile", tileLayout,
		"-geometry", "+0+0",
		"-background", "transparent",
	}

	// Add format-specific options
	switch format {
	case "webp":
		montageArgs = append(montageArgs, "-quality", "80")
	case "png":
		montageArgs = append(montageArgs, "-compress", "zip")
	}

	montageArgs = append(montageArgs, spritePath)

	montageCmd := exec.CommandContext(ctx, "montage", montageArgs...)
	if err := montageCmd.Run(); err != nil {
		return nil, "", fmt.Errorf("failed to create sprite montage: %w", err)
	}

	// Calculate sprite dimensions
	tilesX, _ := strconv.Atoi(tileParts[0])
	tilesY, _ := strconv.Atoi(tileParts[1])
	
	// Estimate frame size (assuming uniform scaling)
	frameWidth := videoWidth / 4  // Scale down for sprite
	frameHeight := videoHeight / 4
	spriteWidth := frameWidth * tilesX
	spriteHeight := frameHeight * tilesY

	spriteInfo := &SpriteInfo{
		Index:      index,
		Filename:   filepath.Base(spritePath),
		StartTime:  startTime,
		EndTime:    endTime,
		FrameCount: actualFrameCount,
		Width:      spriteWidth,
		Height:     spriteHeight,
	}

	// Clean up frame directory
	os.RemoveAll(frameDir)

	return spriteInfo, spritePath, nil
}

func (sg *SpriteGenerator) GetVideoDuration(ctx context.Context, videoPath string) (float64, error) {
	args := []string{
		"-v", "quiet",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		videoPath,
	}

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get video duration: %w", err)
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %w", err)
	}

	return duration, nil
}

func (sg *SpriteGenerator) GetVideoDimensions(ctx context.Context, videoPath string) (int, int, error) {
	args := []string{
		"-v", "quiet",
		"-show_entries", "stream=width,height",
		"-select_streams", "v:0",
		"-of", "csv=p=0",
		videoPath,
	}

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get video dimensions: %w", err)
	}

	dimensions := strings.TrimSpace(string(output))
	parts := strings.Split(dimensions, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid dimensions format: %s", dimensions)
	}

	width, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse width: %w", err)
	}

	height, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse height: %w", err)
	}

	return width, height, nil
}

// CalculateTileDimensions calculates tile size preserving aspect ratio
// maxTileSize is the maximum dimension (width or height) for a tile
func CalculateTileDimensions(width, height int, maxTileSize int) (tileWidth, tileHeight int) {
	aspectRatio := float64(width) / float64(height)
	
	if aspectRatio >= 1 {
		// Landscape or square
		tileWidth = maxTileSize
		tileHeight = int(float64(maxTileSize) / aspectRatio)
	} else {
		// Portrait
		tileHeight = maxTileSize
		tileWidth = int(float64(maxTileSize) * aspectRatio)
	}
	
	return tileWidth, tileHeight
}

// CalculateGridSize calculates optimal square grid for given frame count
func CalculateGridSize(frameCount int) int {
	return int(math.Ceil(math.Sqrt(float64(frameCount))))
}
