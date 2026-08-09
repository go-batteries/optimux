package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/roverxio/optimux/src/encoders"
	"github.com/roverxio/optimux/src/mediahose"
	"github.com/roverxio/optimux/src/shared"
)

// RegisterVideoProcessorFactory registers the video processor factory with mediahose
func RegisterVideoProcessorFactory() {
	log.Println("ffmpeg.RegisterVideoProcessorFactory: Starting registration")
	mediahose.RegisterVideoProcessorFactory(CreateVideoProcessor)
	log.Println("✅ ffmpeg.RegisterVideoProcessorFactory: Registration complete")
}

// RegisterVideoProcessorFactoryWithExecutors registers the enhanced video processor factory with executor support
func RegisterVideoProcessorFactoryWithExecutors(configPath string) {
	mediahose.RegisterVideoProcessorFactory(func(operation string, tempDir string) mediahose.JobProcessor {
		return NewVideoJobProcessorWithExecutors(operation, tempDir, configPath)
	})
}

// CreateVideoProcessor creates a video processor for the mediahose system
func CreateVideoProcessor(operation string, tempDir string) mediahose.JobProcessor {
	processor := NewVideoJobProcessor(operation, tempDir)
	log.Printf("✅ CreateVideoProcessor: Created %T", processor)
	return processor
}

// VideoJobProcessor implements the JobProcessor interface for video processing
type VideoJobProcessor struct {
	Operation       string
	TempDir         string
	ExecutorFactory *ExecutorFactory
	UseExecutors    bool // Flag to enable executor-based processing
}

func NewVideoJobProcessor(operation string, tempDir string) *VideoJobProcessor {
	log.Printf("🆕 NewVideoJobProcessor: operation=%s, tempDir=%s", operation, tempDir)

	// All video processing now uses executors - no traditional processors needed
	log.Printf("🎯 Video processing uses executor-based architecture only")

	vjp := &VideoJobProcessor{
		Operation:    operation,
		TempDir:      tempDir,
		UseExecutors: true, // Always use executors for video processing
	}
	log.Printf("✅ NewVideoJobProcessor: Created executor-only VideoJobProcessor")
	return vjp
}

func NewVideoJobProcessorWithExecutors(operation string, tempDir string, configPath string) *VideoJobProcessor {
	// Create traditional processor as fallback
	vjp := NewVideoJobProcessor(operation, tempDir)

	// Add executor support
	executorFactory := NewExecutorFactory(configPath, tempDir)
	vjp.ExecutorFactory = executorFactory

	log.Printf("✅ NewVideoJobProcessorWithExecutors: Added executor support")
	return vjp
}

func (vjp *VideoJobProcessor) Process(ctx context.Context, job *mediahose.Job) error {
	config := job.GetProcessorConfig()

	log.Printf("🎬 VideoJobProcessor.Process: operation=%s, path=%s, format=%s", vjp.Operation, job.ImagePath, job.Format)
	log.Printf("🔧 Config: UseExecutors=%v, FrameRate=%d, Quality=%d", vjp.UseExecutors, config.FrameRate, config.Quality)

	// Always use executor-based processing for videos
	if vjp.ExecutorFactory != nil {
		log.Printf("⚡ Using executor-based processing")
		return vjp.processWithExecutors(ctx, job, config)
	}

	// No fallback - executors are required for video processing
	return fmt.Errorf("executor factory not initialized - video processing requires executors")
}

// processWithExecutors handles processing using the new executor architecture
func (vjp *VideoJobProcessor) processWithExecutors(ctx context.Context, job *mediahose.Job, config *mediahose.ProcessorConfig) error {
	// Map operation to action name
	actionName := vjp.getActionFromOperation(vjp.Operation)
	if actionName == "" {
		return fmt.Errorf("unsupported operation for executors: %s", vjp.Operation)
	}

	// Create working directory
	workDir := filepath.Join(vjp.TempDir, fmt.Sprintf("%s_%s", actionName, job.ID))
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}
	defer os.RemoveAll(workDir)

	// For executors, we can work directly with S3 paths or download as needed
	inputPath := job.ImagePath // This could be S3 path or local path

	// Generate output path based on action
	outputPath := vjp.generateOutputPath(workDir, job.ID, actionName, job.Format)
	log.Printf("📁 Generated output path: %s", outputPath)

	// Get action handler for this operation
	handler, err := vjp.ExecutorFactory.GetActionHandler(actionName)
	if err != nil {
		return fmt.Errorf("failed to get action handler: %w", err)
	}
	
	// Build parameters using action-specific handler
	additionalParams := handler.BuildParams(job, config)
	log.Printf("🎯 Built params for action '%s': %d parameters", actionName, len(additionalParams))
	
	// Create execution job
	executionJob, err := vjp.ExecutorFactory.CreateExecutionJob(actionName, inputPath, outputPath, additionalParams)
	if err != nil {
		return fmt.Errorf("failed to create execution job: %w", err)
	}

	// Get the correct executor type for this action from config
	actionConfig, err := vjp.ExecutorFactory.configLoader.GetActionConfig(actionName)
	if err != nil {
		return fmt.Errorf("failed to get action config: %w", err)
	}

	// Use the first executor type defined for this action
	if len(actionConfig.Executors) == 0 {
		return fmt.Errorf("no executors defined for action: %s", actionName)
	}
	executorType := actionConfig.Executors[0].Type

	log.Printf("🔧 Using executor type '%s' for action '%s'", executorType, actionName)
	executor, err := vjp.ExecutorFactory.CreateExecutor(actionName, executorType)
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	result, err := executor.Execute(ctx, executionJob)
	if err != nil {
		return fmt.Errorf("executor processing failed: %w", err)
	}

	// Handle results using action-specific handler
	return handler.HandleResult(ctx, job, result)
}

// processTraditional - DEPRECATED: All video processing now uses executors
func (vjp *VideoJobProcessor) processTraditional(ctx context.Context, job *mediahose.Job, config *mediahose.ProcessorConfig) error {
	// Traditional processing removed - executors only
	return fmt.Errorf("traditional processing deprecated - use executors")
}

func (vjp *VideoJobProcessor) GetMediaType() mediahose.MediaType {
	return mediahose.MediaTypeVideo
}
func (vjp *VideoJobProcessor) loadVideo(job *mediahose.Job) (io.ReadCloser, error) {
	// Determine which loader to use based on job configuration
	log.Printf("🔍 loadVideo: ImagePath=%s, S3Bucket=%v, S3Key=%v", job.ImagePath, job.S3Bucket != nil, job.S3Key != nil)

	// Check if ImagePath is a local file first
	if _, err := os.Stat(job.ImagePath); err == nil {
		log.Printf("📁 ImagePath is a local file, opening directly: %s", job.ImagePath)
		file, err := os.Open(job.ImagePath)
		if err != nil {
			log.Printf("❌ Failed to open local file: %v", err)
			return nil, fmt.Errorf("failed to open local file: %w", err)
		}
		log.Printf("✅ Local file opened successfully")
		return file, nil
	}

	// Not a local file, use appropriate loader
	var loader VideoLoadStrategy

	// Use HTTP loader for URL-based video paths
	log.Printf("🌐 Using HTTPVideoLoader for URL: %s", job.ImagePath)
	loader = &HTTPVideoLoader{
		Client: NewHTTPVideoClient(vjp.TempDir),
	}

	// Create a temporary VideoJob for the loader
	videoJob := &VideoJob{
		VideoPath: job.ImagePath, // ImagePath is used for video path too
		S3Bucket:  job.S3Bucket,
		S3Key:     job.S3Key,
		Ctx:       job.Ctx,
	}

	log.Printf("📥 Loading video with loader type: %T", loader)
	reader, err := loader.LoadVideo(videoJob)
	if err != nil {
		log.Printf("❌ Failed to load video: %v", err)
		return nil, err
	}
	log.Printf("✅ Video loaded successfully")
	return reader, nil
}

func (vjp *VideoJobProcessor) handleCompressionResult(ctx context.Context, job *mediahose.Job, result *ProcessingResult) error {
	if len(result.OutputPaths) == 0 {
		return fmt.Errorf("no output files generated")
	}

	compressedPath := result.OutputPaths[0]
	defer os.Remove(compressedPath)

	data, err := os.ReadFile(compressedPath)
	if err != nil {
		return fmt.Errorf("failed to read compressed video: %w", err)
	}

	return job.Encoder(ctx, &encoders.ResponseOpts{
		Filename:   fmt.Sprintf("%s_compressed.mp4", job.ID),
		Format:     ".mp4",
		Data:       data,
		S3Bucket:   job.S3Bucket,
		S3Key:      job.S3Key,
		SkipUpload: job.SkipUpload,
	}, job.Resp)
}

func (vjp *VideoJobProcessor) handleSegmentationResult(ctx context.Context, job *mediahose.Job, result *ProcessingResult, config *mediahose.ProcessorConfig) error {
	segments := make([]map[string]interface{}, 0, len(result.OutputPaths))

	for i, segmentPath := range result.OutputPaths {
		defer os.Remove(segmentPath)

		data, err := os.ReadFile(segmentPath)
		if err != nil {
			continue
		}

		segmentKey := fmt.Sprintf("%s_segment_%03d.mp4", job.ID, i)

		if !job.SkipUpload {
			err = job.Encoder(ctx, &encoders.ResponseOpts{
				Filename: segmentKey,
				Format:   ".mp4",
				Data:     data,
				S3Bucket: job.S3Bucket,
				S3Key:    shared.ToPtr(segmentKey),
			}, nil)

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

	response := map[string]interface{}{
		"video_id":       job.ID,
		"total_segments": len(segments),
		"segments":       segments,
		"metadata":       result.Metadata,
	}

	return shared.WriteJSONResponse(job.Resp, response)
}

func (vjp *VideoJobProcessor) handleFrameExtractionResult(ctx context.Context, job *mediahose.Job, result *ProcessingResult, config *mediahose.ProcessorConfig) error {
	log.Printf("🖼️  handleFrameExtractionResult: %d output files, job.Resp=%v", len(result.OutputPaths), job.Resp != nil)

	// Store output paths in job metadata for retrieval by caller
	if job.Metadata == nil {
		job.Metadata = make(map[string]interface{})
	}
	job.Metadata["output_paths"] = result.OutputPaths
	job.Metadata["frame_count"] = result.FrameCount
	log.Printf("💾 Stored %d output paths in job metadata", len(result.OutputPaths))

	// If no response writer, just return (internal call)
	if job.Resp == nil {
		log.Printf("✅ No response writer - internal call, returning success")
		return nil
	}

	frames := make([]map[string]interface{}, 0, len(result.OutputPaths))

	// Handle pagination
	startIdx := config.PageOffset
	endIdx := startIdx + config.PageSize
	if config.PageSize == 0 {
		config.PageSize = 50 // default
	}
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

		frameRate := config.FrameRate
		if frameRate == 0 {
			frameRate = 13
		}

		frames = append(frames, map[string]interface{}{
			"frame_id":  i,
			"filename":  frameKey,
			"timestamp": float64(i) / float64(frameRate),
			"size":      len(data),
		})
	}

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

// Helper methods for executor support

// getActionFromOperation maps operation strings to action names for executors
func (vjp *VideoJobProcessor) getActionFromOperation(operation string) string {
	switch operation {
	case "compress":
		return "compress_video"
	case "segment":
		return "scene_breakdown"
	case "sprites":
		return "generate_sprites"
	case "extract_frames", "frames":
		return "extract_frames"
	case "probe":
		return "probe_video"
	case "webvtt":
		return "generate_webvtt"
	default:
		return ""
	}
}

// generateOutputPath generates the output file path based on action type
func (vjp *VideoJobProcessor) generateOutputPath(workDir, videoID, actionName, format string) string {
	switch actionName {
	case "generate_sprites":
		if format == "" {
			format = "jpg"
		}
		// Save sprites to EFS to avoid sidecar cleaner deletion
		// Nginx caches the HTTP response in /tmp/shm/edge_cache/ anyway
		// Uses shared.TmpfsCacheDir constant (EFS mount: /tmp/shm/image_cache/)
		spritesDir := filepath.Join(shared.TmpfsCacheDir, "videos/sprites", videoID)
		os.MkdirAll(spritesDir, 0755) // Ensure directory exists

		return filepath.Join(spritesDir, fmt.Sprintf("sprite_sheet.%s", format))
	case "compress_video":
		// Save transcoded videos to EFS cache (persistent storage)
		// Uses shared.TmpfsCacheDir constant (EFS mount: /tmp/shm/image_cache/)
		transcodedDir := filepath.Join(shared.TmpfsCacheDir, "videos/transcoded")
		os.MkdirAll(transcodedDir, 0755) // Ensure directory exists

		return filepath.Join(transcodedDir, fmt.Sprintf("%s_compressed.mp4", videoID))
	case "scene_breakdown":
		return filepath.Join(workDir, fmt.Sprintf("%s_segment_%%03d.mp4", videoID))
	case "extract_frames":
		return filepath.Join(workDir, fmt.Sprintf("frame_%%04d.jpg"))
	case "probe_video":
		return filepath.Join(workDir, fmt.Sprintf("%s_probe.json", videoID))
	case "generate_webvtt":
		return filepath.Join(workDir, fmt.Sprintf("%s_subtitles.vtt", videoID))
	default:
		return filepath.Join(workDir, fmt.Sprintf("%s_output.mp4", videoID))
	}
}

// DEPRECATED: buildExecutorParams - Now handled by ActionHandlers
// This method is kept for reference but should not be used
// Use handler.BuildParams() instead via ExecutorFactory.GetActionHandler()

// DEPRECATED: handleExecutorResult - Now handled by ActionHandlers
// This method is kept for reference but should not be used
// Use handler.HandleResult() instead via ExecutorFactory.GetActionHandler()

// DEPRECATED: handleExecutorCompressionResult - Now handled by CompressionActionHandler
func (vjp *VideoJobProcessor) handleExecutorCompressionResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error {
	if len(result.OutputPaths) == 0 {
		return fmt.Errorf("no output files generated")
	}

	// Handle FFmpeg executor result
	return vjp.handleCompressionResultFromPath(ctx, job, result.OutputPaths[0])
}

// DEPRECATED: handleExecutorSegmentationResult - Now handled by SegmentationActionHandler
func (vjp *VideoJobProcessor) handleExecutorSegmentationResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error {
	// Handle FFmpeg executor result
	return fmt.Errorf("FFmpeg executor segmentation result handling not yet implemented")
}

// DEPRECATED: handleExecutorSpriteResult - Now handled by SpriteActionHandler
func (vjp *VideoJobProcessor) handleExecutorSpriteResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error {
	log.Printf("🎞️  handleExecutorSpriteResult: %d output files", len(result.OutputPaths))

	// Show sprite action config from actions.yaml (with safe type conversion)
	spriteActionConfig, err := vjp.ExecutorFactory.configLoader.GetActionConfig("generate_sprites")
	if err != nil {
		log.Printf("⚠️  Failed to get sprite action config: %v", err)
	} else {
		// Use common safe config extraction method
		tileWidth, tileHeight, spriteFPS, tileLayout := vjp.getSafeConfigValues(spriteActionConfig)

		log.Printf("🎯 SPRITE CONFIG FROM YAML: %s grid, %dx%d tiles, %.1ffps",
			tileLayout, tileWidth, tileHeight, spriteFPS)
		log.Printf("📋 CONFIG DETAILS: tile_width=%d, tile_height=%d, fps=%.1f, layout=%s",
			tileWidth, tileHeight, spriteFPS, tileLayout)
	}

	// Try to get sprite frame count from ffmpeg output or metadata
	spriteFrameCount := "unknown"
	if result.Metadata != nil {
		if output, ok := result.Metadata["output"].(string); ok {
			// Parse ffmpeg output for frame count info if available
			log.Printf("🔍 FFMPEG OUTPUT: %s", output)
		}
	}

	log.Printf("📊 SPRITE COUNT: Generated sprite sheet with %s frames", spriteFrameCount)

	// Files are already generated in permanent location, no copying needed
	if job.Metadata == nil {
		job.Metadata = make(map[string]interface{})
	}
	job.Metadata["output_paths"] = result.OutputPaths
	log.Printf("💾 Stored %d sprite paths in job metadata", len(result.OutputPaths))

	// Executor doesn't handle HTTP responses - just return success
	// The calling VideoFFmpegProcessor will handle the response
	log.Printf("✅ Executor completed - returning success for caller to handle response")
	return nil
}

// DEPRECATED: handleExecutorFrameExtractionResult - Now handled by FrameExtractionActionHandler
func (vjp *VideoJobProcessor) handleExecutorFrameExtractionResult(ctx context.Context, job *mediahose.Job, config *mediahose.ProcessorConfig, result *ExecutionResult) error {
	// Handle FFmpeg executor result
	return fmt.Errorf("FFmpeg executor frame extraction result handling not yet implemented")
}

// DEPRECATED: handleExecutorProbeResult - Now handled by ProbeActionHandler
func (vjp *VideoJobProcessor) handleExecutorProbeResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error {
	log.Printf("🔍 handleExecutorProbeResult: Got probe data")

	// Store probe data in job metadata for use by other operations
	if job.Metadata == nil {
		job.Metadata = make(map[string]interface{})
	}
	job.Metadata["probe_data"] = result.Metadata
	log.Printf("💾 Stored probe data in job metadata")

	// Return probe data as JSON response if there's a response writer
	if job.Resp != nil {
		return shared.WriteJSONResponse(job.Resp, map[string]interface{}{
			"video_id":   job.ID,
			"probe_data": result.Metadata,
		})
	}

	return nil
}

// DEPRECATED: handleExecutorWebVTTResult - Now handled by WebVTTActionHandler
func (vjp *VideoJobProcessor) handleExecutorWebVTTResult(ctx context.Context, job *mediahose.Job, result *ExecutionResult) error {
	log.Printf("📝 handleExecutorWebVTTResult: Starting integrated ffprobe + WebVTT generation")

	// Step 1: Get video metadata using ffprobe
	probeData, err := vjp.runFFProbe(ctx, job)
	if err != nil {
		return fmt.Errorf("failed to probe video for WebVTT generation: %w", err)
	}

	// Step 2: Extract video properties
	videoProps, err := vjp.extractVideoProperties(probeData)
	if err != nil {
		return fmt.Errorf("failed to extract video properties: %w", err)
	}

	log.Printf("🎬 Video properties: duration=%.2fs, fps=%.2f, frames=%d",
		videoProps.Duration, videoProps.FPS, videoProps.TotalFrames)

	// Step 3: Calculate sprite frame positions using job metadata
	frames := vjp.calculateSpriteFrames(job, videoProps)
	log.Printf("🎞️  Calculated %d sprite frames", len(frames))
	log.Printf("📊 WEBVTT COUNT: %d frames (should match sprite sheet frames)", len(frames))

	// Step 4: Generate WebVTT content with dynamic data
	webvttContent, err := vjp.generateDynamicWebVTT(job.ID, frames)
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
		return nil
	}

	return nil
}

// VideoProperties holds extracted video metadata
type VideoProperties struct {
	Duration    float64 `json:"duration"`
	FPS         float64 `json:"fps"`
	TotalFrames int     `json:"total_frames"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
}

// SpriteFrame represents a frame in the sprite sheet
type SpriteFrame struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// runFFProbe executes ffprobe to get video metadata
func (vjp *VideoJobProcessor) runFFProbe(ctx context.Context, job *mediahose.Job) (map[string]interface{}, error) {
	log.Printf("🔍 Running ffprobe for video: %s", job.ImagePath)

	// Create probe execution job
	actionName := "probe_video"
	workDir := filepath.Join(vjp.TempDir, fmt.Sprintf("%s_%s", actionName, job.ID))
	os.MkdirAll(workDir, 0755)

	inputPath := job.ImagePath
	outputPath := vjp.generateOutputPath(workDir, job.ID, actionName, "json")

	additionalParams := map[string]interface{}{
		"format": "json",
	}

	executionJob, err := vjp.ExecutorFactory.CreateExecutionJob(actionName, inputPath, outputPath, additionalParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create probe execution job: %w", err)
	}

	// Get executor and run probe
	actionConfig, err := vjp.ExecutorFactory.configLoader.GetActionConfig(actionName)
	if err != nil {
		return nil, fmt.Errorf("failed to get probe action config: %w", err)
	}

	executorType := actionConfig.Executors[0].Type
	executor, err := vjp.ExecutorFactory.CreateExecutor(actionName, executorType)
	if err != nil {
		return nil, fmt.Errorf("failed to create probe executor: %w", err)
	}

	result, err := executor.Execute(ctx, executionJob)
	if err != nil {
		return nil, fmt.Errorf("probe execution failed: %w", err)
	}

	return result.Metadata, nil
}

// extractVideoProperties parses ffprobe output to extract video properties
func (vjp *VideoJobProcessor) extractVideoProperties(probeData map[string]interface{}) (*VideoProperties, error) {
	// Parse the JSON output from ffprobe
	outputStr, ok := probeData["output"].(string)
	if !ok {
		return nil, fmt.Errorf("no output found in probe data")
	}

	var probeResult map[string]interface{}
	if err := json.Unmarshal([]byte(outputStr), &probeResult); err != nil {
		return nil, fmt.Errorf("failed to parse probe JSON: %w", err)
	}

	// Extract format information
	format, ok := probeResult["format"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no format information in probe result")
	}

	durationStr, ok := format["duration"].(string)
	if !ok {
		return nil, fmt.Errorf("no duration in format information")
	}

	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse duration: %w", err)
	}

	// Extract video stream information
	streams, ok := probeResult["streams"].([]interface{})
	if !ok || len(streams) == 0 {
		return nil, fmt.Errorf("no streams found in probe result")
	}

	var videoStream map[string]interface{}
	for _, stream := range streams {
		if s, ok := stream.(map[string]interface{}); ok {
			if codecType, ok := s["codec_type"].(string); ok && codecType == "video" {
				videoStream = s
				break
			}
		}
	}

	if videoStream == nil {
		return nil, fmt.Errorf("no video stream found")
	}

	// Extract frame rate
	rFrameRate, ok := videoStream["r_frame_rate"].(string)
	if !ok {
		return nil, fmt.Errorf("no frame rate found in video stream")
	}

	fps, err := vjp.parseFrameRate(rFrameRate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse frame rate: %w", err)
	}

	// Extract dimensions
	width, ok := videoStream["width"].(float64)
	if !ok {
		return nil, fmt.Errorf("no width found in video stream")
	}

	height, ok := videoStream["height"].(float64)
	if !ok {
		return nil, fmt.Errorf("no height found in video stream")
	}

	totalFrames := int(duration * fps)

	return &VideoProperties{
		Duration:    duration,
		FPS:         fps,
		TotalFrames: totalFrames,
		Width:       int(width),
		Height:      int(height),
	}, nil
}

// parseFrameRate parses frame rate string like "24/1" to float64
func (vjp *VideoJobProcessor) parseFrameRate(frameRateStr string) (float64, error) {
	parts := strings.Split(frameRateStr, "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid frame rate format: %s", frameRateStr)
	}

	numerator, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse frame rate numerator: %w", err)
	}

	denominator, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse frame rate denominator: %w", err)
	}

	if denominator == 0 {
		return 0, fmt.Errorf("frame rate denominator is zero")
	}

	return numerator / denominator, nil
}

// getSafeConfigValues safely extracts config values handling both int and float64 types
func (vjp *VideoJobProcessor) getSafeConfigValues(actionConfig *ActionConfig) (int, int, float64, string) {
	var tileWidth, tileHeight int
	var spriteFPS float64

	// Safe type conversion - YAML can have int or float64
	if w, ok := actionConfig.Defaults["tile_width"].(int); ok {
		tileWidth = w
	} else if w, ok := actionConfig.Defaults["tile_width"].(float64); ok {
		tileWidth = int(w)
	}

	if h, ok := actionConfig.Defaults["tile_height"].(int); ok {
		tileHeight = h
	} else if h, ok := actionConfig.Defaults["tile_height"].(float64); ok {
		tileHeight = int(h)
	}

	if f, ok := actionConfig.Defaults["fps"].(int); ok {
		spriteFPS = float64(f)
	} else if f, ok := actionConfig.Defaults["fps"].(float64); ok {
		spriteFPS = f
	}

	tileLayout, _ := actionConfig.Defaults["tile_layout"].(string)

	return tileWidth, tileHeight, spriteFPS, tileLayout
}

// calculateSpriteFrames calculates frame positions in sprite sheet using job metadata
func (vjp *VideoJobProcessor) calculateSpriteFrames(job *mediahose.Job, props *VideoProperties) []SpriteFrame {
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
			StartTime: vjp.formatTime(startTime),
			EndTime:   vjp.formatTime(endTime),
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
func (vjp *VideoJobProcessor) formatTime(seconds float64) string {
	hours := int(seconds) / 3600
	minutes := (int(seconds) % 3600) / 60
	secs := int(seconds) % 60
	millis := int((seconds - float64(int(seconds))) * 1000)

	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, secs, millis)
}

// generateDynamicWebVTT generates WebVTT content with calculated frame data
func (vjp *VideoJobProcessor) generateDynamicWebVTT(videoID string, frames []SpriteFrame) (string, error) {
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

// copyFile copies a file from src to dst
func (vjp *VideoJobProcessor) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// handleCompressionResultFromPath handles compression result from a file path
func (vjp *VideoJobProcessor) handleCompressionResultFromPath(ctx context.Context, job *mediahose.Job, outputPath string) error {
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("failed to read compressed video: %w", err)
	}
	defer os.Remove(outputPath)

	return job.Encoder(ctx, &encoders.ResponseOpts{
		Filename:   fmt.Sprintf("%s_compressed.mp4", job.ID),
		Format:     ".mp4",
		Data:       data,
		S3Bucket:   job.S3Bucket,
		S3Key:      job.S3Key,
		SkipUpload: job.SkipUpload,
	}, job.Resp)
}
