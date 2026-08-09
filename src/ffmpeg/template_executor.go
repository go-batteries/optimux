package ffmpeg

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"text/template"
)

// TemplateExecutor handles template-based generation (WebVTT, etc.)
type TemplateExecutor struct {
	TempDir string
}

// NewTemplateExecutor creates a new template executor
func NewTemplateExecutor(tempDir string) *TemplateExecutor {
	return &TemplateExecutor{
		TempDir: tempDir,
	}
}

// Execute processes template-based jobs
func (te *TemplateExecutor) Execute(ctx context.Context, job *ExecutionJob) (*ExecutionResult, error) {
	log.Printf("📝 TemplateExecutor.Execute: action=%s", job.Action)
	
	// Get template content from job parameters
	templateContent, ok := job.Parameters["template"].(string)
	if !ok {
		return nil, fmt.Errorf("template content not found in job parameters")
	}
	
	// Parse template
	tmpl, err := template.New(job.Action).Parse(templateContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}
	
	// Execute template with job parameters
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, job.Parameters); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}
	
	// Ensure output directory exists
	outputDir := filepath.Dir(job.OutputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}
	
	// Write template result to output file
	if err := os.WriteFile(job.OutputPath, buf.Bytes(), 0644); err != nil {
		return nil, fmt.Errorf("failed to write template output: %w", err)
	}
	
	log.Printf("✅ Template executed successfully: %s (%d bytes)", job.OutputPath, buf.Len())
	
	return &ExecutionResult{
		OutputPaths: []string{job.OutputPath},
		Metadata: map[string]interface{}{
			"executor":      "template",
			"action":        job.Action,
			"template_size": buf.Len(),
			"output_file":   job.OutputPath,
		},
	}, nil
}

// GetExecutorType returns the executor type
func (te *TemplateExecutor) GetExecutorType() string {
	return "template"
}
