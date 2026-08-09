package ffmpeg

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"text/template"
)

// CommandExecutor handles CLI command execution (ffmpeg, ffprobe, etc.)
// 
// This is the LOW-LEVEL EXECUTION ENGINE that runs actual shell commands.
// 
// Purpose: Executes actual CLI commands (ffmpeg, ffprobe)
// 
// What it does:
// - Runs shell commands via exec.Command()
// - Template parsing - Processes command templates with variables
// - Direct CLI execution - Executes ffmpeg/ffprobe commands
// - Output capture - Captures stdout/stderr from commands
// 
// Example:
//   // Executes: ffmpeg -i input.mp4 -vf fps=5,scale=160:90,tile=10x5 output.webp
//   executor.Execute(ctx, job)
// 
// Analogy: This is the ENGINE 🚗 (does the actual work)
type CommandExecutor struct {
	TempDir string
}

// NewCommandExecutor creates a new command executor
func NewCommandExecutor(tempDir string) *CommandExecutor {
	return &CommandExecutor{
		TempDir: tempDir,
	}
}

// NewFFmpegExecutor creates a new command executor (legacy compatibility)
func NewFFmpegExecutor(tempDir string) *CommandExecutor {
	return NewCommandExecutor(tempDir)
}

// Execute runs an FFmpeg command based on the execution job
func (e *CommandExecutor) Execute(ctx context.Context, job *ExecutionJob) (*ExecutionResult, error) {
	// Get the command template from parameters
	commandTemplate, ok := job.Parameters["command"].(string)
	if !ok {
	}

	// Prepare template data
	templateData := map[string]interface{}{
		"input":  job.InputPath,
		"output": job.OutputPath,
	}

	// Add all job parameters to template data
	for key, value := range job.Parameters {
		if key != "command" {
			templateData[key] = value
		}
	}

	// Parse and execute template
	tmpl, err := template.New("ffmpeg").Parse(commandTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse command template: %w", err)
	}

	var commandBuf bytes.Buffer
	if err := tmpl.Execute(&commandBuf, templateData); err != nil {
		return nil, fmt.Errorf("failed to execute command template: %w", err)
	}

	commandStr := strings.TrimSpace(commandBuf.String())
	
	// Split command into parts (simple approach - could be improved for complex cases)
	parts := strings.Fields(commandStr)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command generated")
	}

	// Execute the command
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg execution failed: %w, output: %s", err, string(output))
	}

	// Return result
	result := &ExecutionResult{
		OutputPaths: []string{job.OutputPath},
		Metadata: map[string]interface{}{
			"executor":     "ffmpeg",
			"action":       job.Action,
			"command":      commandStr,
			"output":       string(output),
		},
	}

	return result, nil
}

// GetExecutorType returns the executor type
func (e *CommandExecutor) GetExecutorType() string {
	return "exec"
}
