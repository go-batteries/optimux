package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// WriteJSONResponse writes a JSON response to the HTTP response writer
func WriteJSONResponse(w http.ResponseWriter, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(data)
}

// OpenCachedFile opens a cached file if it exists
func OpenCachedFile(path string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return file, nil
}

// CacheResponse caches the response body to a file and returns a reader
func CacheResponse(body io.ReadCloser, cachePath string) (io.ReadCloser, error) {
	defer body.Close()
	
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(GetCacheDir(cachePath), 0755); err != nil {
		return nil, err
	}
	
	// Create cache file
	cacheFile, err := os.Create(cachePath)
	if err != nil {
		return nil, err
	}
	defer cacheFile.Close()
	
	// Copy response to cache file
	if _, err := io.Copy(cacheFile, body); err != nil {
		return nil, err
	}
	
	// Return a new reader for the cached file
	return OpenCachedFile(cachePath)
}

// IsS3URL checks if a URL is an S3 URL
func IsS3URL(url string) bool {
	return strings.Contains(url, ".s3.") && strings.Contains(url, ".amazonaws.com")
}

// ParseS3URL parses an S3 URL and returns bucket and key
func ParseS3URL(url string) (bucket, key string) {
	// Example: https://bucket-name.s3.region.amazonaws.com/path/to/object
	if !IsS3URL(url) {
		return "", ""
	}
	
	// Remove protocol
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	
	parts := strings.SplitN(url, "/", 2)
	if len(parts) < 2 {
		return "", ""
	}
	
	// Extract bucket from hostname
	hostParts := strings.Split(parts[0], ".")
	if len(hostParts) > 0 {
		bucket = hostParts[0]
	}
	
	key = parts[1]
	return bucket, key
}

// GetCacheDir returns the directory part of a cache path
func GetCacheDir(cachePath string) string {
	lastSlash := strings.LastIndex(cachePath, "/")
	if lastSlash == -1 {
		return "."
	}
	return cachePath[:lastSlash]
}

// GenerateImageKey generates a key for image assets
func GenerateImageKey(width, height, quality int, format string) string {
	return fmt.Sprintf("%d_%d_%d%s", width, height, quality, format)
}

// GetVideoDuration returns the duration of a video in seconds using ffprobe
func GetVideoDuration(ctx context.Context, videoPath string) (float64, error) {
	args := []string{
		"-v", "quiet",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		videoPath,
	}

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get video duration: %w", err)
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %w", err)
	}

	return duration, nil
}

// GetVideoDimensions returns the width and height of a video using ffprobe
func GetVideoDimensions(ctx context.Context, videoPath string) (int, int, error) {
	args := []string{
		"-v", "quiet",
		"-show_entries", "stream=width,height",
		"-select_streams", "v:0",
		"-of", "csv=p=0",
		videoPath,
	}

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get video dimensions: %w", err)
	}

	dimensions := strings.TrimSpace(string(output))
	parts := strings.Split(dimensions, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid dimensions format: %s", dimensions)
	}

	width, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse width: %w", err)
	}

	height, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse height: %w", err)
	}

	return width, height, nil
}

// CalculateTileDimensions calculates tile size preserving aspect ratio
// maxTileSize is the maximum dimension (width or height) for a tile
func CalculateTileDimensions(width, height int, maxTileSize int) (tileWidth, tileHeight int) {
	aspectRatio := float64(width) / float64(height)
	
	if aspectRatio >= 1 {
		// Landscape or square
		tileWidth = maxTileSize
		tileHeight = int(float64(maxTileSize) / aspectRatio)
	} else {
		// Portrait
		tileHeight = maxTileSize
		tileWidth = int(float64(maxTileSize) * aspectRatio)
	}
	
	return tileWidth, tileHeight
}

// CalculateGridSize calculates optimal square grid for given frame count
func CalculateGridSize(frameCount int) int {
	return int(math.Ceil(math.Sqrt(float64(frameCount))))
}
