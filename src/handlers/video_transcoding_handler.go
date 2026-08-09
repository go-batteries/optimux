package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/roverxio/optimux/src/config"
	"github.com/roverxio/optimux/src/mediahose"
	"github.com/roverxio/optimux/src/shared"
)

// VideoTranscodingHandler handles video transcoding and delivery
// Maps: /optimux/assets/videos/usr_*/vid_*?format=mp4
// To S3: {env}/videos/usr_*/compressed/vid_*_compressed.mp4
type VideoTranscodingHandler struct {
	S3Client      *s3.Client
	DefaultBucket string
	VideoQueue    chan *mediahose.Job
	VideoScaler   *mediahose.DynamicScaler[*mediahose.Job]
}

// Handle processes video transcoding/delivery requests
func (vth *VideoTranscodingHandler) Handle(cfg *config.Config, pathPrefix, env string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract path: /optimux/assets/videos/usr_123/vid_abc.mp4
		path := strings.TrimPrefix(r.URL.Path, pathPrefix)
		
		// Derive S3 URL similar to image handler
		// S3BaseURL + env + path
		s3BaseUrl := cfg.S3BaseURL
		s3URL, err := url.Parse(s3BaseUrl)
		if err != nil {
			http.Error(w, "Invalid S3 base URL", http.StatusInternalServerError)
			return
		}

		// Build S3 path: {s3BaseUrl}/{env}/videos/usr_123/vid_abc.mp4
		s3URL = s3URL.JoinPath(env, path)
		videoPath := s3URL.String()

		log.Printf("Video S3 path: %s", videoPath)

		// Get format and resolution parameters
		format := r.URL.Query().Get("format")
		if format == "" {
			// Default to mp4 or derive from file extension
			format = filepath.Ext(path)
			if format != "" {
				format = strings.TrimPrefix(format, ".")
			} else {
				format = "mp4"
			}
		}

		preset := r.URL.Query().Get("preset") // e.g., "360p", "720p", "1080p"

		// Determine final S3 path
		var finalS3Path string
		if preset != "" {
			log.Printf("Transcoded version requested: preset=%s", preset)
			// Serve pre-transcoded file from cache or S3
			// Convert: stg/videos/usr_123/vid_abc.mp4
			// To:      stg/videos/usr_123/transcoded/vid_abc_360p.mp4
			finalS3Path = vth.buildTranscodedPath(videoPath, preset, format)
		} else {
			// Serve original
			finalS3Path = videoPath
		}

		log.Printf("Final S3 path: %s", finalS3Path)

		// Extract bucket and key
		bucket, key, ok := shared.ExtractBucketAndKeyFromS3(finalS3Path)
		if !ok {
			http.Error(w, "Invalid S3 path", http.StatusBadRequest)
			return
		}

		// Check if file exists in S3
		ctx := r.Context()
		_, err = vth.S3Client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: &bucket,
			Key:    &key,
		})

		if err != nil {
			log.Printf("Video not found in S3: %s/%s - %v", bucket, key, err)
			http.Error(w, "Video not found", http.StatusNotFound)
			return
		}

		// Redirect to S3 URL or proxy the request
		// For now, redirect to S3 (similar to how images work with CloudFront)
		w.Header().Set("Location", finalS3Path)
		w.Header().Set("Cache-Control", "public, max-age=31536000")
		w.WriteHeader(http.StatusFound)
	}
}

// buildTranscodedPath converts original path to transcoded path with preset
func (vth *VideoTranscodingHandler) buildTranscodedPath(originalPath, preset, format string) string {
	// Extract: https://s3.../stg/videos/usr_123/vid_abc.mp4
	// Return:  https://s3.../stg/videos/usr_123/transcoded/vid_abc_360p.mp4
	
	parts := strings.Split(originalPath, "/")
	
	// Find the video filename
	var videoFile string
	var basePath []string
	for i, part := range parts {
		if strings.HasPrefix(part, "vid_") || strings.HasSuffix(part, ".mp4") || strings.HasSuffix(part, ".webm") {
			videoFile = part
			basePath = parts[:i]
			break
		}
	}

	if videoFile == "" {
		return originalPath // Fallback
	}

	// Remove extension and add preset suffix
	videoID := strings.TrimSuffix(videoFile, filepath.Ext(videoFile))
	transcodedFile := fmt.Sprintf("%s_%s.%s", videoID, preset, format)

	// Rebuild path with /transcoded/ subdirectory
	basePath = append(basePath, "transcoded", transcodedFile)
	
	return strings.Join(basePath, "/")
}
