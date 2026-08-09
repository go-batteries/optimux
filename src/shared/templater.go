package shared

import (
	"regexp"
	"strings"
)

/// Maps a string to a template and returns the placeholder params
/// /user_generated/usr12345/img_ab1234.webp parsed by /user_generated/{usr_id}/*
/// returns user_id: usr12345
/// This is needed to get the batch key for batched image processing

// Convert template to regex with named capture groups and wildcard support
func TemplateToRegex(template string) *regexp.Regexp {
	// Ensure the template starts without multiple slashes
	template = strings.TrimPrefix(template, "/")

	rePattern := regexp.MustCompile(`\{(\w+)\}`).ReplaceAllString(template, `(?P<$1>[^/]+)`)

	rePattern = strings.ReplaceAll(rePattern, "*", `(?P<wildcard>.*)`)

	rePattern = `^/?` + rePattern + `$`

	return regexp.MustCompile(rePattern)
}

// Extract parameters from a given path
func ExtractParams(pattern *regexp.Regexp, path string) map[string]string {
	// Normalize path (remove leading `/` for consistency)
	path = strings.TrimPrefix(path, "/")

	match := pattern.FindStringSubmatch(path)
	if match == nil {
		return nil
	}

	params := make(map[string]string)
	for i, name := range pattern.SubexpNames() {
		if i > 0 && name != "" {
			params[name] = match[i]
		}
	}
	return params
}
