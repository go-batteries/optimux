package mediahose

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/roverxio/optimux/src/shared"
)

// VideoFFmpegProcessor handles video processing with FFmpeg (sprites, transcoding, etc.)
// Similar to ImageProcessor but for videos
type VideoFFmpegProcessor struct {
	TempDir  string
	S3Client *s3.Client
	S3Bucket string
}
// getVideoTempDir returns the temp directory for video processing (similar to image cache)
func (vfp *VideoFFmpegProcessor) getVideoTempDir() string {
	if vfp.TempDir != "" {
		return vfp.TempDir
	}
	// Use shared memory for fast video processing (same as image cache)
	return "/tmp/shm/video_processing"
}

// getVideoTempPath generates a temp file path for video processing (similar to GetCacheFilePath)
func (vfp *VideoFFmpegProcessor) getVideoTempPath(videoURL string) string {
	hash := md5.Sum([]byte(videoURL))
	filename := hex.EncodeToString(hash[:]) + ".mp4"
	return filepath.Join(vfp.getVideoTempDir(), filename)
}

// getVideoCachePath returns the cached video path (in /tmp/video_cache/)
func (vfp *VideoFFmpegProcessor) getVideoCachePath(videoURL string) string {
	hash := md5.Sum([]byte(videoURL))
	ext := filepath.Ext(videoURL)
	if ext == "" {
		ext = ".mp4"
	}
	filename := hex.EncodeToString(hash[:]) + ext
	return filepath.Join(shared.VideoCacheDir, filename)
}

// Process handles video processing based on job format parameter
func (vfp *VideoFFmpegProcessor) Process(ctx context.Context, job *Job) ([]byte, error) {
	defer shared.Bench(fmt.Sprintf("VideoFFmpegProcessor.Process %s", job.ID))()

	format := job.Format

	log.Printf("Processing video: format=%s, path=%s", format, job.ImagePath)

	// Route based on format parameter
	switch format {
	case ".webvtt", "webvtt":
		return vfp.generateWebVTTWithExecutor(ctx, job)

	case ".sprites", "sprites":
		return vfp.generateSprites(ctx, job)

	case ".mp4", "mp4", ".webm", "webm":
		return vfp.transcodeVideo(ctx, job)

	default:
		// Default: just download and return the video
		return vfp.downloadVideo(ctx, job)
	}
}

// generateWebVTT generates WebVTT file with sprite thumbnails
func (vfp *VideoFFmpegProcessor) generateWebVTT(ctx context.Context, job *Job) ([]byte, error) {
	log.Printf("Generating WebVTT for: %s", job.ImagePath)

	// Download video to temp file
	tempVideoPath, err := vfp.downloadToTemp(ctx, job)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tempVideoPath)

	// Generate sprites first (needed for WebVTT)
	spritePaths, err := vfp.generateSpriteFiles(ctx, tempVideoPath, job.ID)
	if err != nil {
		return nil, err
	}

	// Build WebVTT content
	webvtt := vfp.buildWebVTTContent(spritePaths, job)

	return []byte(webvtt), nil
}

// generateWebVTTWithExecutor generates WebVTT using the executor-based VideoJobProcessor
func (vfp *VideoFFmpegProcessor) generateWebVTTWithExecutor(ctx context.Context, job *Job) ([]byte, error) {
	log.Printf("🎬 Generating WebVTT with executor for: %s", job.ImagePath)

	// Download video to temp file
	tempVideoPath, err := vfp.downloadToTemp(ctx, job)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tempVideoPath)

	// Create executor-based video processor for WebVTT generation
	log.Printf("🔍 Creating executor-based video processor for WebVTT generation")
	processor := createVideoProcessor("webvtt", "/tmp/shm/video_processing")
	if processor == nil {
		return nil, fmt.Errorf("failed to create video processor")
	}
	log.Printf("✅ Video processor created: %T", processor)

	// Create job for WebVTT processing
	webvttJob := &Job{
		ID:        job.ID,
		ImagePath: tempVideoPath,
		Format:    "vtt",
		Quality:   80,
		Ctx:       ctx,
		Metadata:  make(map[string]interface{}),
	}

	// Process WebVTT generation
	err = processor.Process(ctx, webvttJob)
	if err != nil {
		return nil, fmt.Errorf("WebVTT generation failed: %w", err)
	}

	// Get WebVTT content from metadata
	webvttContent, ok := webvttJob.Metadata["webvtt_content"].(string)
	if !ok {
		return nil, fmt.Errorf("no WebVTT content found in job metadata")
	}

	log.Printf("✅ Generated WebVTT: %d bytes", len(webvttContent))
	return []byte(webvttContent), nil
}

// generateSprites generates sprite sheets and returns JSON response
func (vfp *VideoFFmpegProcessor) generateSprites(ctx context.Context, job *Job) ([]byte, error) {
	log.Printf("Generating sprites for: %s", job.ImagePath)

	// Get video from cache (don't copy to temp - keep on disk)
	// For videos, we need a DownloadClient. If ImageLoader exists and implements DownloadClient, use it.
	// Otherwise, create a default client.
	var downloadClient DownloadClient
	if job.ImageLoader != nil {
		// Try to use ImageLoader if it implements DownloadClient
		if dc, ok := job.ImageLoader.(DownloadClient); ok {
			downloadClient = dc
		}
	}
	if downloadClient == nil {
		// Create default S3 HTTP client for video downloads
		downloadClient = DefaultS3HTTPClient()
	}
	
	// Time the download
	downloadStart := time.Now()
	videoReader, err := LoadVideoFromURLWithCache(downloadClient, ctx, job.ImagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load video: %w", err)
	}
	defer videoReader.Close()
	downloadDuration := time.Since(downloadStart)
	log.Printf("⏱️  Video download took: %v", downloadDuration)

	// Get the cached video path directly (no copying)
	cachedVideoPath := vfp.getVideoCachePath(job.ImagePath)
	log.Printf("📁 Using cached video: %s", cachedVideoPath)

	// Time the sprite generation
	processingStart := time.Now()
	// Generate sprite files (output will go to /tmp/shm/video_processing/)
	spritePaths, err := vfp.generateSpriteFiles(ctx, cachedVideoPath, job.ID)
	if err != nil {
		return nil, err
	}
	processingDuration := time.Since(processingStart)
	log.Printf("⏱️  Sprite generation took: %v", processingDuration)

	if len(spritePaths) == 0 {
		return nil, fmt.Errorf("no sprite files generated")
	}

	// Return the actual sprite sheet image data (not JSON)
	spriteFilePath := spritePaths[0] // First (and usually only) sprite sheet
	log.Printf("📖 Reading sprite file: %s", spriteFilePath)
	
	// Debug: Check if file exists and list directory contents
	if _, err := os.Stat(spriteFilePath); os.IsNotExist(err) {
		log.Printf("❌ Sprite file does not exist: %s", spriteFilePath)
		
		// List the directory to see what's actually there
		dir := filepath.Dir(spriteFilePath)
		if files, err := os.ReadDir(dir); err == nil {
			log.Printf("📂 Directory contents of %s:", dir)
			for _, file := range files {
				log.Printf("  - %s", file.Name())
			}
		} else {
			log.Printf("❌ Cannot read directory %s: %v", dir, err)
		}
		
		return nil, fmt.Errorf("sprite file does not exist: %s", spriteFilePath)
	}
	
	spriteData, err := os.ReadFile(spriteFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read sprite file %s: %w", spriteFilePath, err)
	}

	totalDuration := downloadDuration + processingDuration
	log.Printf("✅ Returning sprite data: %d bytes", len(spriteData))
	log.Printf("📊 TIMING BREAKDOWN: Total=%v (Download=%v, Processing=%v)", 
		totalDuration, downloadDuration, processingDuration)
	return spriteData, nil
}

// transcodeVideo transcodes video to different resolution/format
func (vfp *VideoFFmpegProcessor) transcodeVideo(ctx context.Context, job *Job) ([]byte, error) {
	log.Printf("Transcoding video: %s", job.ImagePath)

	// Download video to temp file
	tempVideoPath, err := vfp.downloadToTemp(ctx, job)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tempVideoPath)

	// Use the registered video processor for transcoding
	processor := createVideoProcessor("compress", vfp.getVideoTempDir())
	if processor == nil {
		return nil, fmt.Errorf("no video processor available for transcoding")
	}

	// Parse query parameters from job metadata or original path
	width, height, quality := vfp.parseTranscodingParams(job)

	// Create job with transcoding config
	transcodeJob := &Job{
		ID:        job.ID + "_transcoded",
		ImagePath: tempVideoPath, // Use local temp file
		Format:    job.Format,
		MediaType: MediaTypeVideo,
		Ctx:       ctx,
		Metadata:  make(map[string]interface{}),
	}

	// Set processor config for transcoding
	config := &ProcessorConfig{
		Operation: "compress",
		Quality:   quality,
		Width:     width,
		Height:    height,
	}

	transcodeJob.SetProcessorConfig(config)
	transcodeJob.SetProcessor(processor)

	// Process the job (this will call the ffmpeg processor)
	err = processor.Process(ctx, transcodeJob)
	if err != nil {
		return nil, fmt.Errorf("transcoding failed: %w", err)
	}

	// Return the transcoded video data
	return os.ReadFile(tempVideoPath + "_compressed.mp4")
}

// downloadVideo just downloads and returns the video bytes
func (vfp *VideoFFmpegProcessor) downloadVideo(ctx context.Context, job *Job) ([]byte, error) {
	client := DefaultS3HTTPClient()
	byts, err := LoadVideoFromURLWithCache(client, ctx, job.ImagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch video: %w", err)
	}
	defer byts.Close()

	return io.ReadAll(byts)
}

// downloadToTemp downloads video to a temporary file
func (vfp *VideoFFmpegProcessor) downloadToTemp(ctx context.Context, job *Job) (string, error) {
	// Download video
	videoBytes, err := vfp.downloadVideo(ctx, job)
	if err != nil {
		return "", err
	}

	// Ensure temp directory exists
	tempDir := vfp.getVideoTempDir()
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create temp dir %s: %w", tempDir, err)
	}

	// Generate temp file path (similar to GetCacheFilePath for images)
	tempFile := vfp.getVideoTempPath(job.ImagePath)
	if err := os.WriteFile(tempFile, videoBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	return tempFile, nil
}

// parseTranscodingParams parses query parameters for transcoding (res, compress, etc.)
func (vfp *VideoFFmpegProcessor) parseTranscodingParams(job *Job) (width, height, quality int) {
	// Default values
	width, height, quality = 0, 0, 80
	// Parse from job metadata if available (set by handler)
	if job.Metadata != nil {
		if res, ok := job.Metadata["res"].(string); ok {
			switch res {
			case "480":
				width, height = 854, 480 // 480p
			case "720":
				width, height = 1280, 720 // 720p
			case "1080":
				width, height = 1920, 1080 // 1080p
			}
		}

		if compress, ok := job.Metadata["compress"].(string); ok {
			if compress == "true" || compress == "1" {
				quality = 70 // Lower quality for compression
			}
		}

		if q, ok := job.Metadata["quality"].(int); ok {
			quality = q
		}
	}

	log.Printf("Parsed transcoding params: width=%d, height=%d, quality=%d", width, height, quality)
	return width, height, quality
}

// generateSpriteFiles generates sprite sheet images using FFmpeg with executor system
func (vfp *VideoFFmpegProcessor) generateSpriteFiles(ctx context.Context, videoPath, videoID string) ([]string, error) {
	log.Printf("🎞️  generateSpriteFiles: videoPath=%s, videoID=%s", videoPath, videoID)
	
	const (
		fps = 5
		maxDuration = 300.0  // 5 minutes
		maxFrames = 1500     // 5 minutes * 5fps
		maxTileSize = 160    // Max tile dimension
	)
	
	// 1. Get video metadata using shared helper functions
	duration, err := shared.GetVideoDuration(ctx, videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get duration: %w", err)
	}
	
	width, height, err := shared.GetVideoDimensions(ctx, videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get dimensions: %w", err)
	}
	
	log.Printf("📹 Video metadata: duration=%.1fs, dimensions=%dx%d", duration, width, height)
	
	// 2. Limit to 5 minutes
	if duration > maxDuration {
		log.Printf("⚠️  Video is %.1fs (%.1f min), truncating to %.1fs (5 min limit)", 
			duration, duration/60, maxDuration)
		duration = maxDuration
	}
	
	// 3. Calculate tile dimensions (aspect ratio preserved)
	tileWidth, tileHeight := shared.CalculateTileDimensions(width, height, maxTileSize)
	log.Printf("📐 Tile dimensions: %dx%d (aspect ratio: %.2f)", 
		tileWidth, tileHeight, float64(width)/float64(height))
	
	// 4. Calculate grid size
	totalFrames := int(duration * float64(fps))
	gridSize := shared.CalculateGridSize(totalFrames)
	tileLayout := fmt.Sprintf("%dx%d", gridSize, gridSize)
	
	log.Printf("🎯 Grid: %s (%d frames, %d capacity)", 
		tileLayout, totalFrames, gridSize*gridSize)
	
	// Use the executor-based processor with actions.yaml config
	log.Printf("🔍 Creating executor-based video processor for sprite generation")
	
	// Create processor with "sprites" operation (will use executor system with actions.yaml)
	processor := createVideoProcessor("sprites", vfp.getVideoTempDir())
	if processor == nil {
		log.Printf("❌ createVideoProcessor returned nil - factory not registered!")
		return nil, fmt.Errorf("no video processor available for sprite generation")
	}
	log.Printf("✅ Video processor created: %T", processor)

	// Create job with sprite generation config
	job := &Job{
		ID:        videoID,
		ImagePath: videoPath,
		Format:    "webp",
		MediaType: MediaTypeVideo,
		Ctx:       ctx,
		Metadata:  map[string]interface{}{
			"tile_width":  tileWidth,
			"tile_height": tileHeight,
			"tile_layout": tileLayout,
			"grid_size":   gridSize,
			"fps":         fps,
			"duration":    duration,
		},
	}

	// Set processor config for sprite generation (uses actions.yaml defaults)
	config := &ProcessorConfig{
		Operation: "generate_sprites", // Maps to action name in actions.yaml
		FrameRate: fps,
		Quality:   80,
	}

	job.SetProcessorConfig(config)
	job.SetProcessor(processor)

	// Process the job (this will use executor system with actions.yaml)
	err = processor.Process(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("sprite generation failed: %w", err)
	}

	// Extract sprite paths from job metadata
	outputPaths, ok := job.Metadata["output_paths"].([]string)
	if !ok {
		log.Printf("⚠️  No output_paths found in job metadata")
		return []string{}, nil
	}
	
	log.Printf("✅ Generated %d sprite sheets for %s", len(outputPaths), videoID)
	return outputPaths, nil
}

// buildWebVTTContent builds WebVTT file content
func (vfp *VideoFFmpegProcessor) buildWebVTTContent(spritePaths []string, job *Job) string {
	var vtt strings.Builder

	// TODO: Build proper WebVTT with sprite references
	for i, spritePath := range spritePaths {
		startTime := float64(i) * 10.0 // 10 seconds per sprite
		endTime := startTime + 10.0

		vtt.WriteString(fmt.Sprintf("%s --> %s\n",
			shared.FormatTimestamp(startTime),
			shared.FormatTimestamp(endTime)))
		vtt.WriteString(fmt.Sprintf("%s\n\n", spritePath))
	}

	return vtt.String()
}

// cacheSpritesInS3 uploads generated sprites to S3 for caching
func (vfp *VideoFFmpegProcessor) cacheSpritesInS3(ctx context.Context, spritePaths []string, videoID, userID, env string, s3Client *s3.Client, bucket string) error {
	// Upload sprites to S3 for caching
	// Path structure: {env}/videos/usr_{userID}/sprites/{videoID}/
	spritesPrefix := fmt.Sprintf("%s/videos/usr_%s/sprites/%s/", env, userID, videoID)

	for _, spritePath := range spritePaths {
		// Read sprite file
		spriteData, err := os.ReadFile(spritePath)
		if err != nil {
			log.Printf("Failed to read sprite file %s: %v", spritePath, err)
			continue
		}

		// Extract filename
		spriteFilename := filepath.Base(spritePath)

		// Upload to S3
		s3Key := fmt.Sprintf("%s%s", spritesPrefix, spriteFilename)
		_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(s3Key),
			Body:        strings.NewReader(string(spriteData)),
			ContentType: aws.String("image/webp"),
		})

		if err != nil {
			log.Printf("Failed to upload sprite %s to S3: %v", spritePath, err)
			continue
		}

		log.Printf("Cached sprite %s in S3 at %s", spriteFilename, s3Key)
	}

	return nil
}
