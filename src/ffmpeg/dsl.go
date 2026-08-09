package ffmpeg

import (
	"bytes"
	"fmt"
	"text/template"
)

// FFmpegAction defines a templated ffmpeg command
type FFmpegAction struct {
	Name     string                 `yaml:"name"`
	Defaults map[string]interface{} `yaml:"defaults"`
	Command  string                 `yaml:"command"`
	Args     []string               `yaml:"args"`
}

// FFmpegDSL contains all available actions
type FFmpegDSL struct {
	Actions []FFmpegAction `yaml:"actions"`
}

// DefaultFFmpegDSL returns the default DSL configuration
func DefaultFFmpegDSL() *FFmpegDSL {
	return &FFmpegDSL{
		Actions: []FFmpegAction{
			{
				Name: "compress_video",
				Defaults: map[string]interface{}{
					"quality":       23,
					"preset":        "medium",
					"format":        "mp4",
					"audio_bitrate": "128k",
				},
				Command: "ffmpeg -i {{.input}} -c:v libx264 -crf {{.quality}} -preset {{.preset}} -c:a aac -b:a {{.audio_bitrate}} {{.output}}",
				Args:    []string{"input", "output"},
			},
			{
				Name: "extract_frames",
				Defaults: map[string]interface{}{
					"fps":     6,
					"quality": 2,
					"format":  "jpg",
				},
				Command: "ffmpeg -i {{.input}} -vf fps={{.fps}} -q:v {{.quality}} {{.output_pattern}}",
				Args:    []string{"input", "output_pattern"},
			},
			{
				Name: "create_sprite",
				Defaults: map[string]interface{}{
					"tile_layout": "6x5",
					"geometry":    "+0+0",
					"background":  "transparent",
					"quality":     80,
				},
				Command: "montage {{.input_pattern}} -tile {{.tile_layout}} -geometry {{.geometry}} -background {{.background}} -quality {{.quality}} {{.output}}",
				Args:    []string{"input_pattern", "output"},
			},
			{
				Name: "segment_video",
				Defaults: map[string]interface{}{
					"segment_duration": 1.0,
					"codec":           "copy",
				},
				Command: "ffmpeg -i {{.input}} -ss {{.start_time}} -t {{.segment_duration}} -c {{.codec}} -avoid_negative_ts make_zero -y {{.output}}",
				Args:    []string{"input", "start_time", "output"},
			},
			{
				Name: "get_video_info",
				Defaults: map[string]interface{}{
					"format": "json",
				},
				Command: "ffprobe -v quiet -print_format {{.format}} -show_format -show_streams {{.input}}",
				Args:    []string{"input"},
			},
			{
				Name: "extract_thumbnail",
				Defaults: map[string]interface{}{
					"time":    "00:00:01",
					"quality": 2,
					"scale":   "320:240",
				},
				Command: "ffmpeg -i {{.input}} -ss {{.time}} -vframes 1 -vf scale={{.scale}} -q:v {{.quality}} {{.output}}",
				Args:    []string{"input", "output"},
			},
		},
	}
}

// ExecuteAction renders and returns the command for a given action
func (dsl *FFmpegDSL) ExecuteAction(actionName string, params map[string]interface{}) (string, error) {
	action := dsl.findAction(actionName)
	if action == nil {
		return "", fmt.Errorf("action '%s' not found", actionName)
	}

	// Merge defaults with provided parameters
	templateData := make(map[string]interface{})
	for k, v := range action.Defaults {
		templateData[k] = v
	}
	for k, v := range params {
		templateData[k] = v
	}

	// Validate required arguments
	for _, arg := range action.Args {
		if _, exists := templateData[arg]; !exists {
			return "", fmt.Errorf("required argument '%s' missing for action '%s'", arg, actionName)
		}
	}

	// Parse and execute template
	tmpl, err := template.New(actionName).Parse(action.Command)
	if err != nil {
		return "", fmt.Errorf("failed to parse template for action '%s': %w", actionName, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData); err != nil {
		return "", fmt.Errorf("failed to execute template for action '%s': %w", actionName, err)
	}

	return buf.String(), nil
}

func (dsl *FFmpegDSL) findAction(name string) *FFmpegAction {
	for i := range dsl.Actions {
		if dsl.Actions[i].Name == name {
			return &dsl.Actions[i]
		}
	}
	return nil
}

// GetAvailableActions returns a list of all available action names
func (dsl *FFmpegDSL) GetAvailableActions() []string {
	actions := make([]string, len(dsl.Actions))
	for i, action := range dsl.Actions {
		actions[i] = action.Name
	}
	return actions
}

// GetActionInfo returns detailed information about a specific action
func (dsl *FFmpegDSL) GetActionInfo(actionName string) (*FFmpegAction, error) {
	action := dsl.findAction(actionName)
	if action == nil {
		return nil, fmt.Errorf("action '%s' not found", actionName)
	}
	return action, nil
}
