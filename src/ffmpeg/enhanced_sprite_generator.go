package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/roverxio/optimux/src/shared"
)

// EnhancedSpriteGenerator uses the new executor pattern while maintaining compatibility
// with the existing JobProcessor interface
type EnhancedSpriteGenerator struct {
	TempDir         string
	ExecutorFactory *ExecutorFactory
}

// NewEnhancedSpriteGenerator creates a new enhanced sprite generator
func NewEnhancedSpriteGenerator(tempDir, configPath string) *EnhancedSpriteGenerator {
	return &EnhancedSpriteGenerator{
		TempDir:         tempDir,
		ExecutorFactory: NewExecutorFactory(configPath, tempDir),
	}
}

// GenerateSprites creates spritesheets using the configured executor
func (esg *EnhancedSpriteGenerator) GenerateSprites(ctx context.Context, config *ProcessingConfig) (*ProcessingResult, error) {
	defer shared.Bench(fmt.Sprintf("EnhancedSpriteGenerator.GenerateSprites %s", config.VideoID))()

	// Create working directory
	workDir := filepath.Join(esg.TempDir, fmt.Sprintf("sprites_%s", config.VideoID))
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	// Get video metadata first (this could also be done via executor in the future)
	duration, err := esg.getVideoDuration(ctx, config.InputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get video duration: %w", err)
	}

	// Configuration parameters
	fps := 5
	framesPerSprite := 50
	spriteFormat := "webp"

	// Calculate sprite parameters
	totalFrames := int(duration * float64(fps))
	spriteCount := (totalFrames + framesPerSprite - 1) / framesPerSprite

	var sprites []SpriteInfo
	var outputPaths []string

	// Generate each sprite using the executor
	for i := 0; i < spriteCount; i++ {
		startTime := float64(i*framesPerSprite) / float64(fps)
		endTime := float64((i+1)*framesPerSprite) / float64(fps)
		if endTime > duration {
			endTime = duration
		}

		spriteInfo, spritePath, err := esg.generateSingleSpriteWithExecutor(ctx, config, i, startTime, endTime, fps, framesPerSprite, spriteFormat, workDir)
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
		TileLayout:      "6x5",
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
			"executor_type":     esg.ExecutorFactory.GetPreferredExecutor(),
		},
	}, nil
}

// generateSingleSpriteWithExecutor generates a single sprite using the executor pattern
func (esg *EnhancedSpriteGenerator) generateSingleSpriteWithExecutor(ctx context.Context, config *ProcessingConfig, index int, startTime, endTime float64, fps, framesPerSprite int, format string, workDir string) (*SpriteInfo, string, error) {
	// Create output path for the sprite
	spritePath := filepath.Join(workDir, fmt.Sprintf("sprite_%03d.%s", index, format))

	// Create execution job for sprite generation
	additionalParams := map[string]interface{}{
		"fps":        fps,
		"start_time": startTime,
		"duration":   endTime - startTime,
		"format":     format,
		"tile":       "6x5",
		"quality":    "80",
	}

	executionJob, err := esg.ExecutorFactory.CreateExecutionJob("generate_sprites", config.InputPath, spritePath, additionalParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create execution job: %w", err)
	}

	// Get the appropriate executor
	executorType := esg.ExecutorFactory.GetPreferredExecutor()
	executor, err := esg.ExecutorFactory.CreateExecutor("generate_sprites", executorType)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create executor: %w", err)
	}

	// Execute the job
	result, err := executor.Execute(ctx, executionJob)
	if err != nil {
		return nil, "", fmt.Errorf("failed to execute sprite generation: %w", err)
	}

	// Create sprite info
	spriteInfo := &SpriteInfo{
		Index:      index,
		Filename:   filepath.Base(spritePath),
		StartTime:  startTime,
		EndTime:    endTime,
		FrameCount: framesPerSprite,
		Width:      480, // These would be calculated based on actual output
		Height:     270,
	}

	// Use the first output path from the result
	if len(result.OutputPaths) > 0 {
		return spriteInfo, result.OutputPaths[0], nil
	}

	return spriteInfo, spritePath, nil
}

// getVideoDuration is a helper method (could be moved to a utility executor in the future)
func (esg *EnhancedSpriteGenerator) getVideoDuration(ctx context.Context, videoPath string) (float64, error) {
	// For now, use the existing implementation
	// In the future, this could be done via an executor as well
	sg := &SpriteGenerator{TempDir: esg.TempDir}
	return sg.GetVideoDuration(ctx, videoPath)
}
