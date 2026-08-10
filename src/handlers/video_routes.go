package handlers

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/ffmpeg"
	"github.com/go-batteries/optimux/src/mediahose"
	"github.com/go-batteries/optimux/src/shared"
)

// VideoRouteHandler manages video processing routes and workers
type VideoRouteHandler struct {
	VideoHandler    *ffmpeg.VideoHandler
	VideoScaler     *mediahose.DynamicScaler[*ffmpeg.VideoJob]
	BatchScaler     *mediahose.DynamicScaler[*ffmpeg.VideoBatchedJob]
	TempDir         string
	DefaultBucket   string
}

// NewVideoRouteHandler creates a new video route handler with workers
func NewVideoRouteHandler(s3Client *s3.Client, encoder encoders.Encoder, defaultBucket string) *VideoRouteHandler {
	tempDir := filepath.Join(os.TempDir(), "ffmpeg_processing")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Printf("Failed to create temp directory: %v", err)
	}

	// Create video job queue
	videoQueue := make(chan *ffmpeg.VideoJob, 100)
	batchQueue := make(chan *ffmpeg.VideoBatchedJob, 50)

	// Create processor factory for video processing
	factory := mediahose.NewDefaultProcessorFactory()
	
	// Create enhanced batch queue for mediahose jobs
	enhancedBatchQueue := make(chan *mediahose.BatchedJob, 50)
	
	// Create mediahose dispatcher for polymorphic processing
	dispatcher := mediahose.NewDispatcher(
		200*time.Millisecond,
		enhancedBatchQueue,
		func(batch *mediahose.BatchedJob) {
			log.Printf("Batch %s pushed to queue with %d jobs", batch.UID, len(batch.Jobs))
		},
	)
	
	// Enhanced worker factory will be used by the batch scaler
	_ = factory // Keep factory for potential future use

	// Create video loader
	videoLoader := &ffmpeg.HTTPVideoLoader{
		Client: ffmpeg.NewHTTPVideoClient(tempDir),
	}

	// Create video handler with mediahose dispatcher and enhanced workers
	videoHandler := ffmpeg.NewVideoHandler(
		videoQueue, batchQueue, dispatcher, encoder, 
		videoLoader, defaultBucket, tempDir,
	)

	// Create dynamic scalers for workers
	videoScaler := mediahose.BootStrapDynamicScalerFrom(&mediahose.DynamicScaler[*ffmpeg.VideoJob]{
		WorkerFactory:      ffmpeg.VideoWorkerFactory(tempDir),
		Queue:              videoQueue,
		MinWorkers:         2,
		MaxWorkers:         10,
		ScaleUpThreshold:   5,
		ScaleDownThreshold: 1,
		ScaleSigChan:       make(chan struct{}, 1),
		Name:               "VideoWorker",
	})

	batchScaler := mediahose.BootStrapDynamicScalerFrom(&mediahose.DynamicScaler[*ffmpeg.VideoBatchedJob]{
		WorkerFactory:      ffmpeg.BatchVideoWorkerFactory(tempDir, true),
		Queue:              batchQueue,
		MinWorkers:         1,
		MaxWorkers:         5,
		ScaleUpThreshold:   3,
		ScaleDownThreshold: 0,
		ScaleSigChan:       make(chan struct{}, 1),
		Name:               "BatchVideoWorker",
	})

	return &VideoRouteHandler{
		VideoHandler:  videoHandler,
		VideoScaler:   videoScaler,
		BatchScaler:   batchScaler,
		TempDir:       tempDir,
		DefaultBucket: defaultBucket,
	}
}

// StartWorkers starts the video processing workers
func (vrh *VideoRouteHandler) StartWorkers(ctx context.Context) {
	log.Println("Starting video processing workers...")
	vrh.VideoScaler.Start(ctx)
	vrh.BatchScaler.Start(ctx)
}

// RegisterRoutes registers video processing routes
func (vrh *VideoRouteHandler) RegisterRoutes(mux *http.ServeMux) {
	// Video compression endpoint
	mux.HandleFunc("/api/video/compress", vrh.VideoHandler.HandleVideoCompress)
	
	// Video segmentation endpoint  
	mux.HandleFunc("/api/video/segment", vrh.VideoHandler.HandleVideoSegment)
	
	// Frame extraction endpoint with pagination
	mux.HandleFunc("/api/video/frames", vrh.VideoHandler.HandleExtractFrames)
	
	// Health check for video processing
	mux.HandleFunc("/api/video/health", vrh.handleVideoHealth)
}

// handleVideoHealth provides health check for video processing system
func (vrh *VideoRouteHandler) handleVideoHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	health := map[string]interface{}{
		"status": "healthy",
		"workers": map[string]interface{}{
			"video_workers": map[string]interface{}{
				"active":     vrh.VideoScaler.ActiveCount(),
				"queue_size": len(vrh.VideoScaler.Queue),
				"min":        vrh.VideoScaler.MinWorkers,
				"max":        vrh.VideoScaler.MaxWorkers,
			},
			"batch_workers": map[string]interface{}{
				"active":     vrh.BatchScaler.ActiveCount(),
				"queue_size": len(vrh.BatchScaler.Queue),
				"min":        vrh.BatchScaler.MinWorkers,
				"max":        vrh.BatchScaler.MaxWorkers,
			},
		},
		"temp_dir": vrh.TempDir,
		"timestamp": time.Now().UTC(),
	}

	shared.WriteJSONResponse(w, health)
}
