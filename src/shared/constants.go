package shared

import "time"

var AllowedImgExtMap = map[string]string{
	".jpg":  "image/jpg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".tiff": "image/tiff",
	".png":  "image/png",
}

var VideoExtMap = map[string]string{
	".mp4":     "video/mp4",
	".avi":     "video/avi",
	".mkv":     "video/mkv",
	".mov":     "video/mov",
	".webm":    "video/webm",
	// Video processing formats
	".sprites": "image/webp",                     // Video sprite sheets (output as WebP images)
	".webvtt":  "text/vtt",                      // WebVTT subtitle format
	".hls":     "application/vnd.apple.mpegurl", // HLS playlist
	".dash":    "application/dash+xml",          // DASH manifest
	".m3u8":    "application/vnd.apple.mpegurl", // M3U8 playlist
}

const TmpfsCacheDir = "/tmp/shm/image_cache"
const VideoCacheDir = "/tmp/video_cache" // Videos are large, cache on disk not RAM

const (
	DefaultWaitTillEnQTime  = 2 * time.Second
	DefaultContentTypeImage = "image/*"
)

var IdSizeMap = map[string]string{
	"@600w": "600x", // 2x denotes, 2 times smaller
	"@300w": "300x",
	"@30w":  "30x",
}

var IdSizeMapRev = map[string]string{
	"600x": "@600w",
	"300x": "@300w",
	"30x":  "@30w",
}

// var SizesForCompression = []string{
// 	"600x", "300x", "30x",
// }

var SizesForCompression = []string{
	"@600w", "@300w", "@30w",
}

const (
	MediaPrefixImage = "img_"
)

const (
	S3HeaderProcessedMedia = "x-media-processed"
)
