package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-batteries/optimux/src/config"
	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/ffmpeg"
	"github.com/go-batteries/optimux/src/mediahose"
	"github.com/go-batteries/optimux/src/shared"
)

// VideoSpriteHandler handles video sprite and WebVTT requests
// Generates sprites on-the-fly using worker queue (like image processing)
type VideoSpriteHandler struct {
	S3Client      *s3.Client
	DefaultBucket string
	Env           string
	VideoQueue    chan *mediahose.Job
	VideoScaler   *mediahose.DynamicScaler[*mediahose.Job]
	Encoder       encoders.Encoder // Add encoder
}

// SpriteResponse represents the JSON response with sprite information
type SpriteResponse struct {
	VideoID  string                 `json:"video_id"`
	UserID   string                 `json:"user_id"`
	Sprites  []SpriteInfo           `json:"sprites"`
	WebVTT   string                 `json:"webvtt_url"`
	Manifest *ffmpeg.SpriteManifest `json:"manifest,omitempty"`
}

// SpriteInfo contains sprite file information
type SpriteInfo struct {
	URL       string  `json:"url"`
	Index     int     `json:"index"`
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
}

// Handle processes video sprite requests - generates sprites ON-THE-FLY using worker queue
func (vsh *VideoSpriteHandler) Handle(cfg *config.Config, pathPrefix, env string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		
		// Extract path: /optimux/assets/videos/usr_123/vid_abc.mp4?format=webvtt
		path := strings.TrimPrefix(r.URL.Path, pathPrefix)

		// Check if it's a video request with format=webvtt or sprites
		format := r.URL.Query().Get("format")
		if format != "webvtt" && format != "sprites" {
			http.NotFound(w, r)
			return
		}

		// Parse path: videos/usr_123/vid_abc.mp4
		if !strings.HasPrefix(path, "/videos/usr_") {
			http.Error(w, "Invalid video path", http.StatusBadRequest)
			return
		}

		// Build S3 video path
		s3BaseUrl := cfg.S3BaseURL
		s3URL, err := url.Parse(s3BaseUrl)
		if err != nil {
			http.Error(w, "Invalid S3 base URL", http.StatusInternalServerError)
			return
		}

		// Join: https://bucket.s3.amazonaws.com + /stg/videos/usr_123/vid_abc.mp4
		s3URL = s3URL.JoinPath(env, path)
		videoPath := s3URL.String()

		log.Printf("Generating sprites for video: %s", videoPath)

		// Extract filename for job ID
		fileName, _, ok := shared.ExplodeFileName(videoPath)
		if !ok {
			log.Println("invalid video url for videoPath", videoPath)
			http.Error(w, "invalid video url", http.StatusBadRequest)
			return
		}

		// Create job for sprite generation (similar to image processing)
		job := &mediahose.Job{
			ID:              fileName,
			ImagePath:       videoPath, // Reusing ImagePath for video
			Format:          format,    // "webvtt" or "sprites"
			Quality:         80,        // Default quality for sprites
			Resp:            w,
			Ctx:             ctx,
			Done:            make(shared.DoneCh),
			Encoder:         vsh.Encoder, // Add the missing encoder!
			ErrHandler:      shared.ResponseWriter,
			MediaType:       mediahose.MediaTypeVideo,
			OrigPath:        r.URL.String(),
			SkipResize:      false, // Always generate sprites
			MailBox:         make(shared.MailBoxCh, 1),
			DefaultS3Bucket: cfg.DefaultS3Bucket,
			Metadata:        make(map[string]interface{}),
		}

		// Push job to worker queue (on-the-fly processing!)
		select {
		case vsh.VideoQueue <- job:
			// Check if we need to scale up workers
			queueUsage := float64(len(vsh.VideoQueue)) / float64(cap(vsh.VideoQueue))
			if queueUsage > 0.75 && vsh.VideoScaler.ActiveCount() < vsh.VideoScaler.MaxWorkers {
				log.Printf("⚠️  Video queue at %.2f%% capacity, scaling up!", queueUsage*100)
				vsh.VideoScaler.ScaleSigChan <- struct{}{}
			}

		case <-time.After(shared.DefaultWaitTillEnQTime):
			http.Error(w, "Server too busy", http.StatusServiceUnavailable)
			return
		}

		// Wait for worker to complete processing
		<-job.Done
		log.Println("Sprite generation completed")
	}
}

// listSprites lists sprite files from S3
func (vsh *VideoSpriteHandler) listSprites(ctx context.Context, prefix string) ([]string, *ffmpeg.SpriteManifest, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(vsh.DefaultBucket),
		Prefix: aws.String(prefix),
	}

	result, err := vsh.S3Client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, nil, err
	}

	var sprites []string
	var manifestKey string

	for _, obj := range result.Contents {
		key := *obj.Key
		if strings.HasSuffix(key, ".webp") || strings.HasSuffix(key, ".jpg") || strings.HasSuffix(key, ".png") {
			sprites = append(sprites, key)
		} else if strings.HasSuffix(key, "manifest.json") {
			manifestKey = key
		}
	}

	// Load manifest if exists
	var manifest *ffmpeg.SpriteManifest
	if manifestKey != "" {
		manifest, _ = vsh.loadManifest(ctx, manifestKey)
	}

	return sprites, manifest, nil
}

// loadManifest loads sprite manifest from S3
func (vsh *VideoSpriteHandler) loadManifest(ctx context.Context, key string) (*ffmpeg.SpriteManifest, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(vsh.DefaultBucket),
		Key:    aws.String(key),
	}

	result, err := vsh.S3Client.GetObject(ctx, input)
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()

	var manifest ffmpeg.SpriteManifest
	if err := json.NewDecoder(result.Body).Decode(&manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// buildSpriteResponse builds the JSON response
func (vsh *VideoSpriteHandler) buildSpriteResponse(videoID, userID string, spriteKeys []string, manifest *ffmpeg.SpriteManifest, baseURL, pathPrefix, env string) *SpriteResponse {
	sprites := make([]SpriteInfo, 0, len(spriteKeys))

	for i, key := range spriteKeys {
		// Convert S3 key to public URL
		// {env}/videos/usr_123/sprites/vid_abc/sprite_001.webp
		// -> https://media-{env}.example.com/optimux/assets/videos/usr_123/sprites/vid_abc/sprite_001.webp
		relativePath := strings.TrimPrefix(key, env+"/")
		url := fmt.Sprintf("%s%s/%s", baseURL, pathPrefix, relativePath)

		spriteInfo := SpriteInfo{
			URL:   url,
			Index: i,
		}

		// Add timing info from manifest if available
		if manifest != nil && i < len(manifest.Sprites) {
			spriteInfo.StartTime = manifest.Sprites[i].StartTime
			spriteInfo.EndTime = manifest.Sprites[i].EndTime
		}

		sprites = append(sprites, spriteInfo)
	}

	// WebVTT URL
	webvttURL := fmt.Sprintf("%s%s/videos/usr_%s/%s.mp4?format=webvtt", baseURL, pathPrefix, userID, videoID)

	return &SpriteResponse{
		VideoID:  videoID,
		UserID:   userID,
		Sprites:  sprites,
		WebVTT:   webvttURL,
		Manifest: manifest,
	}
}

// generateWebVTT generates WebVTT content for video thumbnails
func (vsh *VideoSpriteHandler) generateWebVTT(spriteKeys []string, manifest *ffmpeg.SpriteManifest, baseURL, pathPrefix, env, userID, videoID string) string {
	var vtt strings.Builder
	vtt.WriteString("WEBVTT\n\n")

	fps := 5.0 // Default
	if manifest != nil {
		fps = float64(manifest.FPS)
	}

	for i, key := range spriteKeys {
		relativePath := strings.TrimPrefix(key, env+"/")
		url := fmt.Sprintf("%s%s/%s", baseURL, pathPrefix, relativePath)

		var startTime, endTime float64
		if manifest != nil && i < len(manifest.Sprites) {
			startTime = manifest.Sprites[i].StartTime
			endTime = manifest.Sprites[i].EndTime
		} else {
			// Fallback calculation
			startTime = float64(i) * (1.0 / fps)
			endTime = startTime + (1.0 / fps)
		}

		// Format: 00:00:00.000 --> 00:00:01.000
		vtt.WriteString(fmt.Sprintf("%s --> %s\n",
			shared.FormatTimestamp(startTime),
			shared.FormatTimestamp(endTime)))
		vtt.WriteString(fmt.Sprintf("%s\n\n", url))
	}

	return vtt.String()
}
