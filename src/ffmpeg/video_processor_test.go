package ffmpeg

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-batteries/optimux/src/mediahose"
)

const (
	testVideoPath = "/tmp/video_cache/2187cb44e6768551e8e34162f152f7eb.mp4"
	testTempDir   = "/tmp/shm/video_processing"
	testConfigPath = "/Users/darksied/dev/pocs/optimux-resty/config/actions.yaml"
)

func TestVideoSpriteGeneration(t *testing.T) {
	// Check if test video exists
	if _, err := os.Stat(testVideoPath); os.IsNotExist(err) {
		t.Skipf("Test video not found: %s", testVideoPath)
	}

	// Create video processor with executors
	processor := NewVideoJobProcessorWithExecutors("sprites", testTempDir, testConfigPath)
	
	// Create test job
	job := &mediahose.Job{
		ID:        "test_vid_sprites",
		ImagePath: testVideoPath,
		Format:    "webp",
		Quality:   80,
		Ctx:       context.Background(),
		Metadata:  make(map[string]interface{}),
	}

	t.Logf("🎬 Testing sprite generation for: %s", testVideoPath)
	
	// Process the video
	start := time.Now()
	err := processor.Process(context.Background(), job)
	duration := time.Since(start)
	
	if err != nil {
		t.Fatalf("Sprite generation failed: %v", err)
	}

	t.Logf("✅ Sprite generation completed in %v", duration)
	
	// Check if output paths are stored in metadata
	outputPaths, ok := job.Metadata["output_paths"].([]string)
	if !ok || len(outputPaths) == 0 {
		t.Fatalf("No output paths found in job metadata")
	}

	t.Logf("📁 Generated sprite files: %v", outputPaths)

	// Verify sprite file exists and has content
	for _, spritePath := range outputPaths {
		if _, err := os.Stat(spritePath); os.IsNotExist(err) {
			t.Errorf("Sprite file does not exist: %s", spritePath)
			continue
		}

		// Check file size
		fileInfo, err := os.Stat(spritePath)
		if err != nil {
			t.Errorf("Failed to get file info for %s: %v", spritePath, err)
			continue
		}

		if fileInfo.Size() == 0 {
			t.Errorf("Sprite file is empty: %s", spritePath)
			continue
		}

		t.Logf("✅ Sprite file: %s (size: %d bytes)", spritePath, fileInfo.Size())
	}
}

func TestVideoProbeGeneration(t *testing.T) {
	// Check if test video exists
	if _, err := os.Stat(testVideoPath); os.IsNotExist(err) {
		t.Skipf("Test video not found: %s", testVideoPath)
	}

	// Create video processor with executors
	processor := NewVideoJobProcessorWithExecutors("probe", testTempDir, testConfigPath)
	
	// Create test job
	job := &mediahose.Job{
		ID:        "test_vid_probe",
		ImagePath: testVideoPath,
		Format:    "json",
		Ctx:       context.Background(),
		Metadata:  make(map[string]interface{}),
	}

	t.Logf("🔍 Testing video probe for: %s", testVideoPath)
	
	// Process the video
	start := time.Now()
	err := processor.Process(context.Background(), job)
	duration := time.Since(start)
	
	if err != nil {
		t.Fatalf("Video probe failed: %v", err)
	}

	t.Logf("✅ Video probe completed in %v", duration)
	
	// Check if probe data is stored in metadata
	probeData, ok := job.Metadata["probe_data"]
	if !ok {
		t.Fatalf("No probe data found in job metadata")
	}

	t.Logf("📊 Probe data: %+v", probeData)
}

func TestVideoWebVTTGeneration(t *testing.T) {
	// Check if test video exists
	if _, err := os.Stat(testVideoPath); os.IsNotExist(err) {
		t.Skipf("Test video not found: %s", testVideoPath)
	}

	// Create video processor with executors
	processor := NewVideoJobProcessorWithExecutors("webvtt", testTempDir, testConfigPath)
	
	// Create test job
	job := &mediahose.Job{
		ID:        "test_vid_webvtt",
		ImagePath: testVideoPath,
		Format:    "vtt",
		Ctx:       context.Background(),
		Metadata:  make(map[string]interface{}),
	}

	t.Logf("📝 Testing WebVTT generation for: %s", testVideoPath)
	
	// Process the video
	start := time.Now()
	err := processor.Process(context.Background(), job)
	duration := time.Since(start)
	
	if err != nil {
		t.Fatalf("WebVTT generation failed: %v", err)
	}

	t.Logf("✅ WebVTT generation completed in %v", duration)
	
	// Check if WebVTT content is stored in metadata
	webvttContent, ok := job.Metadata["webvtt_content"].(string)
	if !ok || webvttContent == "" {
		t.Fatalf("No WebVTT content found in job metadata")
	}

	t.Logf("📝 WebVTT content preview (first 200 chars):\n%s", 
		truncateString(webvttContent, 200))

	// Verify WebVTT format
	if !strings.Contains(webvttContent, "WEBVTT") {
		t.Errorf("WebVTT content does not contain WEBVTT header")
	}

	if !strings.Contains(webvttContent, "-->") {
		t.Errorf("WebVTT content does not contain timestamp markers")
	}
}

func TestVideoProcessorIntegration(t *testing.T) {
	// Check if test video exists
	if _, err := os.Stat(testVideoPath); os.IsNotExist(err) {
		t.Skipf("Test video not found: %s", testVideoPath)
	}

	t.Logf("🧪 Running integration test with video: %s", testVideoPath)

	// Test 1: Generate sprites first
	t.Run("Sprites", func(t *testing.T) {
		TestVideoSpriteGeneration(t)
	})

	// Test 2: Probe video metadata
	t.Run("Probe", func(t *testing.T) {
		TestVideoProbeGeneration(t)
	})

	// Test 3: Generate WebVTT
	t.Run("WebVTT", func(t *testing.T) {
		TestVideoWebVTTGeneration(t)
	})

	t.Logf("✅ All integration tests completed")
}

// Helper function to truncate strings for logging
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Benchmark sprite generation
func BenchmarkSpriteGeneration(b *testing.B) {
	if _, err := os.Stat(testVideoPath); os.IsNotExist(err) {
		b.Skipf("Test video not found: %s", testVideoPath)
	}

	processor := NewVideoJobProcessorWithExecutors("sprites", testTempDir, testConfigPath)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job := &mediahose.Job{
			ID:        "bench_vid_sprites",
			ImagePath: testVideoPath,
			Format:    "webp",
			Quality:   80,
			Ctx:       context.Background(),
			Metadata:  make(map[string]interface{}),
		}

		err := processor.Process(context.Background(), job)
		if err != nil {
			b.Fatalf("Sprite generation failed: %v", err)
		}
	}
}
