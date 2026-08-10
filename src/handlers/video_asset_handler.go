package handlers

import (
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-batteries/optimux/src/config"
	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/mediahose"
)

// VideoAssetHandler routes video requests to appropriate handlers based on format parameter
// Similar to how image handler works, but for videos
type VideoAssetHandler struct {
	S3Client      *s3.Client
	DefaultBucket string
	VideoScaler   *mediahose.DynamicScaler[*mediahose.Job]

	// Sub-handlers for different video operations
	SpriteHandler      *VideoSpriteHandler
	TranscodingHandler *VideoTranscodingHandler
}

// NewVideoAssetHandler creates a new video asset handler
func NewVideoAssetHandler(s3Client *s3.Client, defaultBucket string, videoQueue chan *mediahose.Job, videoScaler *mediahose.DynamicScaler[*mediahose.Job], encoder encoders.Encoder) *VideoAssetHandler {
	return &VideoAssetHandler{
		S3Client:      s3Client,
		DefaultBucket: defaultBucket,
		VideoScaler:   videoScaler,
		SpriteHandler: &VideoSpriteHandler{
			S3Client:      s3Client,
			DefaultBucket: defaultBucket,
			VideoQueue:    videoQueue,
			VideoScaler:   videoScaler,
			Encoder:       encoder, // Add the encoder!
		},
		TranscodingHandler: &VideoTranscodingHandler{
			S3Client:      s3Client,
			DefaultBucket: defaultBucket,
			VideoQueue:    videoQueue,
			VideoScaler:   videoScaler,
		},
	}
}

// Handle routes video requests based on format parameter
// Follows the same pattern as S3ProxyImageHandler
func (vah *VideoAssetHandler) Handle(cfg *config.Config, pathPrefix, env string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("🎬 VideoAssetHandler.Handle: URL=%s, pathPrefix=%s", r.URL.Path, pathPrefix)
		// Extract path: /optimux/assets/videos/usr_123/vid_abc.mp4
		path := strings.TrimPrefix(r.URL.Path, pathPrefix)
		log.Printf("🎬 Extracted path: %s", path)

		// Only handle video paths
		if !strings.HasPrefix(path, "/videos/") {
			log.Printf("❌ Path doesn't start with /videos/: %s", path)
			http.NotFound(w, r)
			return
		}
		log.Printf("✅ Valid video path: %s", path)

		videoPath := filepath.Join(env, path)

		// Get format and preset parameters
		format := r.URL.Query().Get("format")
		preset := r.URL.Query().Get("preset") // e.g., "360p", "720p", "1080p"

		log.Printf("Video request: path=%s, format=%s, preset=%s", videoPath, format, preset)

		// Route to appropriate handler based on format
		log.Printf("🔀 Routing to handler for format: %s", format)
		switch format {
		case "webvtt", "sprites":
			// Sprite/WebVTT handler for video scrubbing
			vah.SpriteHandler.Env = env
			vah.SpriteHandler.Handle(cfg, pathPrefix, env)(w, r)

		case "mp4", "webm", "hls", "dash", "":
			// Transcoding handler for video delivery
			// If resolution is specified, transcode on-the-fly
			// Empty format means serve the original or default transcoded version
			vah.TranscodingHandler.Handle(cfg, pathPrefix, env)(w, r)

		default:
			http.Error(w, "Unsupported format", http.StatusBadRequest)
		}
	}
}
