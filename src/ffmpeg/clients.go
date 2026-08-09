package ffmpeg

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/roverxio/optimux/src/shared"
)

// S3VideoClientImpl implements S3VideoClient for downloading videos from S3
type S3VideoClientImpl struct {
	S3Client *s3.Client
}

func NewS3VideoClient(s3Client *s3.Client) *S3VideoClientImpl {
	return &S3VideoClientImpl{
		S3Client: s3Client,
	}
}

func (s3c *S3VideoClientImpl) DownloadVideo(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	input := &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}

	result, err := s3c.S3Client.GetObject(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to download video from S3: %w", err)
	}

	return result.Body, nil
}

// HTTPVideoClientImpl implements HTTPVideoClient for downloading videos from HTTP URLs
type HTTPVideoClientImpl struct {
	HTTPClient *http.Client
	CacheDir   string
}

func NewHTTPVideoClient(cacheDir string) *HTTPVideoClientImpl {
	return &HTTPVideoClientImpl{
		HTTPClient: &http.Client{
			Timeout: 5 * time.Minute, // Longer timeout for video downloads
		},
		CacheDir: cacheDir,
	}
}

func (hc *HTTPVideoClientImpl) DownloadVideo(ctx context.Context, url string) (io.ReadCloser, error) {
	// Check cache first
	cachedPath := shared.GetCacheFilePath(url)
	if cachedFile, err := shared.OpenCachedFile(cachedPath); err == nil {
		return cachedFile, nil
	}

	// Download from URL
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := hc.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download video: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	// Cache the video for future use
	cachedReader, err := shared.CacheResponse(resp.Body, cachedPath)
	if err != nil {
		// If caching fails, return the original response
		return resp.Body, nil
	}

	return cachedReader, nil
}

// NewVideoLoaderFromConfig creates appropriate video loader based on video path
func NewVideoLoaderFromConfig(videoPath string, s3Client *s3.Client, cacheDir string) VideoLoadStrategy {
	if shared.IsS3URL(videoPath) {
		bucket, key := shared.ParseS3URL(videoPath)
		return &S3VideoLoader{
			Client: &S3VideoClientWrapper{
				S3Client: s3Client,
				Bucket:   bucket,
				Key:      key,
			},
		}
	}
	
	return &HTTPVideoLoader{
		Client: NewHTTPVideoClient(cacheDir),
	}
}

// S3VideoClientWrapper wraps S3VideoClientImpl to provide bucket/key context
type S3VideoClientWrapper struct {
	S3Client *s3.Client
	Bucket   string
	Key      string
}

func (wrapper *S3VideoClientWrapper) DownloadVideo(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	// Use provided bucket/key if specified, otherwise use wrapper's values
	if bucket == "" {
		bucket = wrapper.Bucket
	}
	if key == "" {
		key = wrapper.Key
	}
	
	client := NewS3VideoClient(wrapper.S3Client)
	return client.DownloadVideo(ctx, bucket, key)
}
