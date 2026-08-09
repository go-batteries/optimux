package ffmpeg

import (
	"fmt"
	"os"
)

// ExecutorFactory creates executors based on configuration
type ExecutorFactory struct {
	configLoader   *ConfigLoader
	tempDir        string
	actionHandlers map[string]ActionHandler // Registry of action handlers
}

// NewExecutorFactory creates a new executor factory
func NewExecutorFactory(configPath, tempDir string) *ExecutorFactory {
	ef := &ExecutorFactory{
		configLoader:   NewConfigLoader(configPath),
		tempDir:        tempDir,
		actionHandlers: make(map[string]ActionHandler),
	}
	
	// Register default action handlers
	ef.RegisterActionHandler(NewSpriteActionHandler())
	ef.RegisterActionHandler(NewWebVTTActionHandler())
	ef.RegisterActionHandler(NewCompressionActionHandler())
	ef.RegisterActionHandler(NewSegmentationActionHandler())
	ef.RegisterActionHandler(NewProbeActionHandler())
	ef.RegisterActionHandler(NewFrameExtractionActionHandler())
	
	return ef
}

// RegisterActionHandler registers an action handler
func (ef *ExecutorFactory) RegisterActionHandler(handler ActionHandler) {
	ef.actionHandlers[handler.GetActionName()] = handler
}

// GetActionHandler retrieves an action handler by name
func (ef *ExecutorFactory) GetActionHandler(actionName string) (ActionHandler, error) {
	handler, ok := ef.actionHandlers[actionName]
	if !ok {
		return nil, fmt.Errorf("no handler registered for action: %s", actionName)
	}
	return handler, nil
}

// CreateExecutor creates an executor for the specified action and type
func (ef *ExecutorFactory) CreateExecutor(actionName, executorType string) (Executor, error) {
	_, err := ef.configLoader.GetExecutorConfig(actionName, executorType)
	if err != nil {
		return nil, fmt.Errorf("failed to get executor config: %w", err)
	}

	switch executorType {
	case "exec":
		// Generic command executor (ffmpeg, ffprobe, etc.)
		return NewCommandExecutor(ef.tempDir), nil
	case "ffmpeg":
		// Legacy support - redirect to exec
		return NewCommandExecutor(ef.tempDir), nil
	case "template":
		// Template-based generation
		return NewTemplateExecutor(ef.tempDir), nil
	case "mediaconvert":
		// AWS MediaConvert (future)
		return nil, fmt.Errorf("mediaconvert executor not yet implemented")
	case "lambda":
		// Serverless processing (future)
		return nil, fmt.Errorf("lambda executor not yet implemented")
	case "gpu":
		// GPU-accelerated processing (future)
		return nil, fmt.Errorf("gpu executor not yet implemented")
	default:
		return nil, fmt.Errorf("unsupported executor type: %s", executorType)
	}
}
func (ef *ExecutorFactory) GetPreferredExecutor() string {
	// Check environment variable first
	if executorType := os.Getenv("VIDEO_EXECUTOR"); executorType != "" {
		return executorType
	}
	
	// Default to ffmpeg
	return "ffmpeg"
}

// CreateExecutionJob creates an ExecutionJob from the configuration
func (ef *ExecutorFactory) CreateExecutionJob(actionName, inputPath, outputPath string, additionalParams map[string]interface{}) (*ExecutionJob, error) {
	// Get action config to determine the correct executor type
	actionConfig, err := ef.configLoader.GetActionConfig(actionName)
	if err != nil {
		return nil, err
	}
	
	// Use the first executor type defined for this action
	if len(actionConfig.Executors) == 0 {
		return nil, fmt.Errorf("no executors defined for action: %s", actionName)
	}
	executorType := actionConfig.Executors[0].Type
	
	executorConfig, err := ef.configLoader.GetExecutorConfig(actionName, executorType)
	if err != nil {
		return nil, err
	}

	// Merge configuration parameters with additional parameters
	parameters := make(map[string]interface{})
	for key, value := range executorConfig.Parameters {
		parameters[key] = value
	}
	for key, value := range additionalParams {
		parameters[key] = value
	}

	return &ExecutionJob{
		Action:     actionName,
		InputPath:  inputPath,
		OutputPath: outputPath,
		Parameters: parameters,
	}, nil
}
