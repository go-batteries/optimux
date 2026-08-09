package ffmpeg

import (
	"context"
)

// ExecutionJob represents a single execution task for an executor
type ExecutionJob struct {
	Action     string
	InputPath  string
	OutputPath string
	Parameters map[string]interface{}
}

// ExecutionResult holds the output of an execution
type ExecutionResult struct {
	OutputPaths []string
	Metadata    map[string]interface{}
}

// Executor interface for different video processing backends
// This works alongside the existing JobProcessor interface
type Executor interface {
	Execute(ctx context.Context, job *ExecutionJob) (*ExecutionResult, error)
	GetExecutorType() string
}

// ExecutorConfig holds configuration for creating executors
type ExecutorConfig struct {
	Type       string                 `yaml:"type"`
	Command    string                 `yaml:"command,omitempty"`
	Template   string                 `yaml:"template,omitempty"`
	PipelineID string                 `yaml:"pipelineId,omitempty"`
	PresetID   string                 `yaml:"presetId,omitempty"`
	Parameters map[string]interface{} `yaml:"parameters,omitempty"`
}

// ActionConfig represents a video processing action with multiple executor options
type ActionConfig struct {
	Name      string                 `yaml:"name"`
	Defaults  map[string]interface{} `yaml:"defaults"`
	Executors []ExecutorConfig       `yaml:"executors"`
}

// ActionsConfig represents the full configuration file
type ActionsConfig struct {
	Actions []ActionConfig `yaml:"actions"`
}
