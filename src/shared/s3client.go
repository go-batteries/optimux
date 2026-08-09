package shared

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

type S3Client struct {
	client *http.Client
}

var httpClient = &http.Client{
	Timeout: 60 * time.Second, // Increased for video downloads (can be large files)
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second, // Fast header response
	},
}

func DefaultS3Client() *S3Client {
	return &S3Client{
		client: httpClient,
	}
}

func (s3 *S3Client) DownloadImage(ctx context.Context, imageURL, cachePath string) ([]byte, error) {
	log.Println("downloading image from", imageURL)

	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}

	resp, err := s3.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("external api response failed with status. %d", resp.StatusCode)
	}

	file, err := os.Create(cachePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create tmpfs cache file: %v", err)
	}
	defer file.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to write image to tmpfs: %v", err)
	}

	_, err = file.Write(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to write image to tmpfs: %v", err)
	}

	return buf, nil
}

func (s3 *S3Client) DownloadVideo(ctx context.Context, imageURL, cachePath string) (io.ReadCloser, error) {
	log.Println("downloading video from", imageURL)

	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}

	resp, err := s3.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download failed with status code. %d", resp.StatusCode)
	}

	file, err := os.Create(cachePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create tmpfs cache file: %v", err)
	}
	defer file.Close()

	// TODO: check io.Pipe

	buf := &bytes.Buffer{}
	multiWriter := io.MultiWriter(file, buf)
	if _, err := io.Copy(multiWriter, resp.Body); err != nil {
		return nil, fmt.Errorf("failed to write image to tmpfs: %v", err)
	}

	return io.NopCloser(buf), nil
}
