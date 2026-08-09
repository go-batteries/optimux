package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFFmpegExecutor(t *testing.T) {
	// Skip if ffmpeg is not available
	if _, err := os.Stat("/usr/bin/ffmpeg"); os.IsNotExist(err) {
		if _, err := os.Stat("/usr/local/bin/ffmpeg"); os.IsNotExist(err) {
			t.Skip("ffmpeg not found, skipping test")
		}
	}

	tempDir := t.TempDir()
	executor := NewFFmpegExecutor(tempDir)

	// Create a simple test job
	job := &ExecutionJob{
		Action:     "test_compress",
		InputPath:  "test_input.mp4",
		OutputPath: filepath.Join(tempDir, "test_output.mp4"),
		Parameters: map[string]interface{}{
			"command": "ffmpeg -f lavfi -i testsrc=duration=1:size=320x240:rate=1 -c:v libx264 -t 1 {{.output}}",
		},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, job)

	if err != nil {
		t.Fatalf("Executor failed: %v", err)
	}

	if len(result.OutputPaths) == 0 {
		t.Fatal("No output paths returned")
	}

	// Check if output file exists
	if _, err := os.Stat(result.OutputPaths[0]); os.IsNotExist(err) {
		t.Fatalf("Output file does not exist: %s", result.OutputPaths[0])
	}

	// Verify metadata
	if result.Metadata["executor"] != "ffmpeg" {
		t.Errorf("Expected executor type 'ffmpeg', got %v", result.Metadata["executor"])
	}
}

func TestConfigLoader(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_actions.yaml")

	configContent := `actions:
  - name: test_action
    defaults:
      quality: 23
    executors:
      - type: ffmpeg
        command: "ffmpeg -i {{.input}} -o {{.output}}"
      - type: elastictranscoder
        pipelineId: "test-pipeline"
        presetId: "test-preset"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	loader := NewConfigLoader(configPath)

	// Test loading config
	config, err := loader.LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if len(config.Actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(config.Actions))
	}

	// Test getting action config
	actionConfig, err := loader.GetActionConfig("test_action")
	if err != nil {
		t.Fatalf("Failed to get action config: %v", err)
	}

	if actionConfig.Name != "test_action" {
		t.Errorf("Expected action name 'test_action', got %s", actionConfig.Name)
	}

	// Test getting executor config
	executorConfig, err := loader.GetExecutorConfig("test_action", "ffmpeg")
	if err != nil {
		t.Fatalf("Failed to get executor config: %v", err)
	}

	if executorConfig.Type != "ffmpeg" {
		t.Errorf("Expected executor type 'ffmpeg', got %s", executorConfig.Type)
	}

	// Check that defaults are merged
	if executorConfig.Parameters["quality"] != 23 {
		t.Errorf("Expected quality 23, got %v", executorConfig.Parameters["quality"])
	}
}

func TestExecutorFactory(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_actions.yaml")

	configContent := `actions:
  - name: test_action
    defaults:
      quality: 23
    executors:
      - type: ffmpeg
        command: "ffmpeg -i {{.input}} -o {{.output}}"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	factory := NewExecutorFactory(configPath, tempDir)

	// Test creating FFmpeg executor
	executor, err := factory.CreateExecutor("test_action", "ffmpeg")
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	if executor.GetExecutorType() != "ffmpeg" {
		t.Errorf("Expected executor type 'ffmpeg', got %s", executor.GetExecutorType())
	}

	// Test creating execution job
	job, err := factory.CreateExecutionJob("test_action", "input.mp4", "output.mp4", map[string]interface{}{
		"custom_param": "value",
	})
	if err != nil {
		t.Fatalf("Failed to create execution job: %v", err)
	}

	if job.Action != "test_action" {
		t.Errorf("Expected action 'test_action', got %s", job.Action)
	}

	if job.Parameters["quality"] != 23 {
		t.Errorf("Expected quality 23, got %v", job.Parameters["quality"])
	}

	if job.Parameters["custom_param"] != "value" {
		t.Errorf("Expected custom_param 'value', got %v", job.Parameters["custom_param"])
	}
}
