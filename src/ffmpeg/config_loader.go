package ffmpeg

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// ConfigLoader loads and parses action configurations
type ConfigLoader struct {
	configPath string
}

// NewConfigLoader creates a new configuration loader
func NewConfigLoader(configPath string) *ConfigLoader {
	return &ConfigLoader{
		configPath: configPath,
	}
}

// LoadConfig loads the actions configuration from YAML file
func (cl *ConfigLoader) LoadConfig() (*ActionsConfig, error) {
	data, err := os.ReadFile(cl.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config ActionsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	return &config, nil
}

// GetActionConfig returns the configuration for a specific action
func (cl *ConfigLoader) GetActionConfig(actionName string) (*ActionConfig, error) {
	config, err := cl.LoadConfig()
	if err != nil {
		return nil, err
	}

	for _, action := range config.Actions {
		if action.Name == actionName {
			return &action, nil
		}
	}

	return nil, fmt.Errorf("action '%s' not found in configuration", actionName)
}

// GetExecutorConfig returns the executor configuration for a specific action and executor type
func (cl *ConfigLoader) GetExecutorConfig(actionName, executorType string) (*ExecutorConfig, error) {
	actionConfig, err := cl.GetActionConfig(actionName)
	if err != nil {
		return nil, err
	}

	for _, executor := range actionConfig.Executors {
		if executor.Type == executorType {
			// Merge defaults with executor-specific parameters
			mergedParams := make(map[string]interface{})
			
			// Add defaults first
			for key, value := range actionConfig.Defaults {
				mergedParams[key] = value
			}
			
			// Add executor-specific parameters (they override defaults)
			for key, value := range executor.Parameters {
				mergedParams[key] = value
			}
			
			// Add command and other executor-specific fields to parameters
			if executor.Command != "" {
				mergedParams["command"] = executor.Command
			}
			if executor.Template != "" {
				mergedParams["template"] = executor.Template
			}
			if executor.PipelineID != "" {
				mergedParams["pipelineId"] = executor.PipelineID
			}
			if executor.PresetID != "" {
				mergedParams["presetId"] = executor.PresetID
			}
			
			executor.Parameters = mergedParams
			return &executor, nil
		}
	}

	return nil, fmt.Errorf("executor type '%s' not found for action '%s'", executorType, actionName)
}
