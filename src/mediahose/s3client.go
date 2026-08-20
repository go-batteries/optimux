package mediahose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-batteries/optimux/src/shared"
)

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

type DownloadClient interface {
	// TODO: can be replaced with Download
	DownloadImage(ctx context.Context, imageURL, cachePath string) ([]byte, error)
	DownloadVideo(ctx context.Context, imageURL, cachePath string) (io.ReadCloser, error)
}

type S3HTTPClient struct {
	httpClient *http.Client
}

func DefaultS3HTTPClient() *S3HTTPClient {
	return &S3HTTPClient{
		httpClient: httpClient,
	}
}

func (s3 *S3HTTPClient) DownloadImage(ctx context.Context, imageURL, cachePath string) ([]byte, error) {
	log.Println("downloading image from", imageURL)

	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}

	resp, err := s3.httpClient.Do(req)
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

	// Single pass over the network stream: every chunk read from
	// resp.Body is written to the tmpfs cache file as it arrives, via
	// TeeReader, instead of buffering the whole response in memory first
	// and only then writing it to disk. Same bytes moved either way, but
	// this overlaps the disk write with the download instead of doing
	// them strictly one after the other.
	tee := io.TeeReader(resp.Body, file)

	buf, err := io.ReadAll(tee)
	if err != nil {
		return nil, fmt.Errorf("failed to read/cache image: %v", err)
	}

	return buf, nil
}

func (s3 *S3HTTPClient) DownloadVideo(ctx context.Context, imageURL, cachePath string) (io.ReadCloser, error) {
	log.Println("downloading video from", imageURL)

	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}

	resp, err := s3.httpClient.Do(req)
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

	// buf, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to write image to tmpfs: %v", err)
	// }
	//
	// _, err = file.Write(buf)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to write image to tmpfs: %v", err)
	// }

	return io.NopCloser(buf), nil
}

type S3BasicClient struct {
	s3Client *s3.Client
	bucket   string
	region   string
}

func NewS3BasicClient(s3Client *s3.Client, bucket, region string) *S3BasicClient {
	return &S3BasicClient{
		s3Client,
		bucket,
		region,
	}
}

func (client *S3BasicClient) Download(ctx context.Context, imageURL, cachePath string) (*bytes.Buffer, error) {
	imageURL = strings.TrimPrefix(imageURL, "/")

	result, err := client.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: shared.ToPtr(client.bucket),
		Key:    shared.ToPtr(imageURL),
	})
	if err != nil {
		var noKey *types.NoSuchKey
		if errors.As(err, &noKey) {
			log.Printf("Can't get object %s from bucket %s. No such key exists.\n", imageURL, client.bucket)
			err = noKey
		} else {
			log.Printf("Couldn't get object bucket: %v, %v. Here's why: %v\n", client.bucket, imageURL, err)
		}
		return nil, err
	}

	defer result.Body.Close()

	file, err := os.Create(cachePath)
	if err != nil {
		log.Printf("Couldn't create file %v. Here's why: %v\n", cachePath, err)
		return nil, err
	}

	defer file.Close()

	buf := &bytes.Buffer{}
	multiWriter := io.MultiWriter(file, buf)
	if _, err := io.Copy(multiWriter, result.Body); err != nil {
		return nil, fmt.Errorf("failed to write image to tmpfs: %v", err)
	}

	return buf, nil
}

func (client *S3BasicClient) DownloadImage(ctx context.Context, imageURL, cachePath string) ([]byte, error) {
	buf, err := client.Download(ctx, imageURL, cachePath)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (client *S3BasicClient) DownloadVideo(ctx context.Context, imageURL, cachePath string) (io.ReadCloser, error) {
	buf, err := client.Download(ctx, imageURL, cachePath)
	if err != nil {
		return nil, err
	}

	return io.NopCloser(buf), nil
}
