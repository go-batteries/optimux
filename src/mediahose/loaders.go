package mediahose

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/roverxio/optimux/src/shared"
)

// LoadImageStrategy defines the contract for different image-loading strategies
type LoadImageStrategy interface {
	LoadImage(job *Job) (*vips.ImageRef, error)
}

// TODO: Add an S3 Image Loader that can deal with pre-signed url

// ThumbnailStrategy uses vips.ThumbnailFromFile for smaller images

// ThumbnailStrategy uses vips.ThumbnailFromFile for smaller images
type ThumbnailStrategy struct {
	httpClient *S3HTTPClient
}

func NewThumbMediaLoader() *ThumbnailStrategy {
	return &ThumbnailStrategy{
		httpClient: DefaultS3HTTPClient(),
	}
}

func (t *ThumbnailStrategy) LoadImage(job *Job) (*vips.ImageRef, error) {
	log.Printf("Using ThumbnailStrategy for image: %s", job.ImagePath)

	width, height := job.Sizes[0][0], job.Sizes[0][1]

	// If width or height is missing, estimate it
	if width == 0 && height > 0 {
		width = height
	} else if height == 0 && width > 0 {
		height = width
	}
	// img, err := vips.NewThumbnailFromFile(job.ImagePath, width, height, vips.InterestingNone)
	img, err := LoadImageFromURLWithCache(t.httpClient, job.ImagePath, width, height, true)
	if err != nil {
		return nil, fmt.Errorf("failed to load thumbnail: %v", err)
	}

	job.Sizes[0] = [2]int{width, height}

	return img.Copy()
}

// FullImageStrategy uses vips_resize for larger images
type FullImageStrategy struct {
	dwnClient DownloadClient
}

func NewImageMediaLoader() *FullImageStrategy {
	return &FullImageStrategy{
		dwnClient: DefaultS3HTTPClient(),
	}
}

func NewMediaLoader(client DownloadClient) *FullImageStrategy {
	return &FullImageStrategy{
		dwnClient: client,
	}
}

func (f *FullImageStrategy) LoadImage(job *Job) (*vips.ImageRef, error) {
	log.Printf("Using FullImageStrategy for image: %s", job.ImagePath)

	width, height := job.Sizes[0][0], job.Sizes[0][1]

	// Open the image file
	// img, err := vips.NewImageFromFile(job.ImagePath)

	img, err := LoadImageFromURLWithCache(f.dwnClient, job.ImagePath, width, height, false)
	if err != nil {
		return nil, fmt.Errorf("failed to load image: %v", err)
	}

	if width == 0 && height == 0 {
		log.Println("width and height is 0. so scaling is not needed")
		return img, nil
	}

	if job.SkipResize {
		log.Println("skipping resizing")
		return img, nil
	}

	originalWidth := img.Width()
	originalHeight := img.Height()
	scale := 1.0

	// Preserve aspect ratio
	scaleWidth := float64(width) / float64(originalWidth)
	scaleHeight := float64(height) / float64(originalHeight)

	// Calculate the scaling factor if one dimension is missing
	if width > 0 && height == 0 {
		height = int(float64(originalHeight) * scaleWidth)
		scale = scaleWidth
	} else if height > 0 && width == 0 {
		width = int(float64(originalWidth) * scaleHeight)
		scale = scaleHeight
	} else if width > 0 && height > 0 {
		scale = math.Min(scaleWidth, scaleHeight)
	}

	// Resize image
	job.Sizes[0] = [2]int{width, height}

	err = img.Resize(scale, vips.KernelLinear) // vips.KernelLanczos2
	// img.Copy()
	if err != nil {
		return nil, err
	}

	return img, nil
}

func LoadVideoFromURLWithCache(client DownloadClient, ctx context.Context, videoURL string) (io.ReadCloser, error) {
	// Videos are large - cache on disk, not RAM
	if err := os.MkdirAll(shared.VideoCacheDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create video cache dir: %v", err)
	}

	// Generate cache path for video (use VideoCacheDir instead of TmpfsCacheDir)
	hash := md5.Sum([]byte(videoURL))
	ext := filepath.Ext(videoURL)
	if ext == "" {
		ext = ".mp4"
	}
	filename := hex.EncodeToString(hash[:]) + ext
	cachePath := filepath.Join(shared.VideoCacheDir, filename)

	if _, err := os.Stat(cachePath); err == nil {
		log.Println("video loaded from cache", cachePath)
		return os.Open(cachePath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // Longer timeout for videos
	defer cancel()

	buf, err := client.DownloadVideo(ctx, videoURL, cachePath)
	return buf, err
}

/// Image Processors

func LoadImageFromURLWithCache(client DownloadClient, imageURL string, width, height int, isThumb bool) (*vips.ImageRef, error) {
	if err := os.MkdirAll(shared.TmpfsCacheDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create tmpfs cache dir: %v", err)
	}

	cachePath := shared.GetCacheFilePath(imageURL)

	loader := LoadImageFromTmpFS
	if isThumb {
		loader = LoadThumbFromTmpFS
	}

	_, err := os.Stat(cachePath)
	var ref *vips.ImageRef

	if err == nil {
		log.Println("loading image from cache")
		ref, err = loader(cachePath, width, height)
	}

	if err == nil {
		log.Println("image loaded from cache")
		return ref, nil
	}

	log.Println("failed to load image, downloading. error", err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	buf, err := client.DownloadImage(ctx, imageURL, cachePath)
	if err != nil {
		return nil, err
	}

	return vips.NewImageFromBuffer(buf)
}

func LoadImageFromTmpFS(imagePath string, width, height int) (*vips.ImageRef, error) {
	ref, err := vips.NewImageFromFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image from tmpfs: %v", err)
	}

	return ref, nil
}

func LoadThumbFromTmpFS(imagePath string, width, height int) (*vips.ImageRef, error) {
	ref, err := vips.NewThumbnailFromFile(imagePath, width, height, vips.InterestingNone)
	if err != nil {
		return nil, fmt.Errorf("failed to read image from tmpfs: %v", err)
	}

	return ref, nil
}
