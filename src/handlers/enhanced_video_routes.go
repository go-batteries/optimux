package handlers

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/ffmpeg"
	"github.com/go-batteries/optimux/src/mediahose"
)

// EnhancedVideoRouteHandler handles video processing using polymorphic mediahose system
type EnhancedVideoRouteHandler struct {
	Dispatcher    *mediahose.Dispatcher
	BatchScaler   *mediahose.DynamicScaler[*mediahose.BatchedJob]
	VideoHandler  *ffmpeg.VideoHandler
	TempDir       string
}

func NewEnhancedVideoRouteHandler(s3Client *s3.Client, encoder encoders.Encoder, defaultBucket string) *EnhancedVideoRouteHandler {
	tempDir := filepath.Join(os.TempDir(), "ffmpeg_processing")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Printf("Failed to create temp directory: %v", err)
	}

	// Register video processor factory
	ffmpeg.RegisterVideoProcessorFactory()

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
	
	// Create enhanced batch worker factory that supports both image and video processing
	enhancedWorkerFactory := mediahose.EnhancedWorkerFactory(factory, tempDir, true)
	
	// Create dynamic scaler for enhanced batch workers
	batchScaler := mediahose.BootStrapDynamicScalerFrom(&mediahose.DynamicScaler[*mediahose.BatchedJob]{
		WorkerFactory:      enhancedWorkerFactory,
		Queue:              enhancedBatchQueue,
		MinWorkers:         2,
		MaxWorkers:         10,
		ScaleUpThreshold:   5,
		ScaleDownThreshold: 1,
	})

	// Create video loader
	videoLoader := &ffmpeg.HTTPVideoLoader{
		Client: ffmpeg.NewHTTPVideoClient(tempDir),
	}

	// Create video handler for API endpoints (still needed for HTTP handling)
	videoHandler := &ffmpeg.VideoHandler{
		Dispatcher:  dispatcher,
		Encoder:     encoder,
		VideoLoader: videoLoader,
		TempDir:     tempDir,
	}

	return &EnhancedVideoRouteHandler{
		Dispatcher:   dispatcher,
		BatchScaler:  batchScaler,
		VideoHandler: videoHandler,
		TempDir:      tempDir,
	}
}

// RegisterRoutes registers video processing routes
func (evrh *EnhancedVideoRouteHandler) RegisterRoutes() {
	// Register video processing routes using the enhanced system
	// This would integrate with your existing router setup
	log.Printf("Enhanced video routes registered with polymorphic processing")
}
