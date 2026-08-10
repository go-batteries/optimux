package ffmpeg

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-batteries/optimux/src/mediahose"
)

// VideoWorker processes video jobs using the existing worker pattern
type VideoWorker struct {
	Idx         int64
	CloserChan  chan int64
	TempDir     string
	Processors  map[ProcessingOperation]FFmpegProcessor
	VideoLoader VideoLoadStrategy
}

func NewVideoWorker(idx int64, closerChan chan int64, tempDir string) *VideoWorker {
	// Ensure temp directory exists
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Printf("Failed to create temp directory %s: %v", tempDir, err)
	}

	return &VideoWorker{
		Idx:        idx,
		CloserChan: closerChan,
		TempDir:    tempDir,
		Processors: map[ProcessingOperation]FFmpegProcessor{
			OperationCompress:      NewVideoCompressionProcessor(tempDir),
			OperationSegment:       NewVideoSegmentProcessor(tempDir),
			OperationExtractFrames: NewFrameExtractionProcessor(tempDir),
		},
	}
}

func (vw *VideoWorker) Work(ctx context.Context, jobQueueChan <-chan *VideoJob) {
	defer func() {
		vw.CloserChan <- vw.Idx
	}()

	for {
		select {
		case <-ctx.Done():
			log.Printf("VideoWorker %d exiting...", vw.Idx)
			return
		case job := <-jobQueueChan:
			log.Printf("VideoWorker %d processing job: %s, operation: %s", vw.Idx, job.ID, job.Operation)

			// Convert VideoJob to mediahose.Job for polymorphic processing
			mediaJob := &mediahose.Job{
				ID:              job.ID,
				ImagePath:       job.VideoPath, // Use VideoPath as ImagePath for compatibility
				Format:          "mp4", // Default video format
				Quality:         job.Quality,
				MediaType:       mediahose.MediaTypeVideo,
				Resp:            job.Resp,
				Done:            job.Done,
				MailBox:         job.MailBox,
				Ctx:             job.Ctx,
				CancelCtx:       job.CancelCtx,
				Encoder:         job.Encoder,
				DefaultS3Bucket: job.DefaultS3Bucket,
				SkipResize:      true,
				SkipUpload:      job.SkipUpload,
				Metadata:        make(map[string]interface{}),
			}

			// Set operation in metadata
			mediaJob.Metadata["operation"] = string(job.Operation)
			if job.Config != nil {
				mediaJob.Metadata["config"] = job.Config
			}

			jobProcessor := NewVideoJobProcessor(string(job.Operation), vw.TempDir)
			
			if err := jobProcessor.Process(ctx, mediaJob); err != nil {
				log.Printf("Video processing failed for job %s: %v", job.ID, err)
				job.ErrHandler(job.Resp, "Video processing failed", 500)
				close(job.Done)
				continue
			}

			log.Printf("VideoWorker %d completed job: %s", vw.Idx, job.ID)
			close(job.Done)
		}
	}
}

// BatchVideoWorker processes batched video jobs
type BatchVideoWorker struct {
	Idx         int64
	CloserChan  chan int64
	TempDir     string
	ConsumeResp bool
	Processors  map[ProcessingOperation]FFmpegProcessor
}

func NewBatchVideoWorker(idx int64, closerChan chan int64, tempDir string, consumeResp bool) *BatchVideoWorker {
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Printf("Failed to create temp directory %s: %v", tempDir, err)
	}

	return &BatchVideoWorker{
		Idx:         idx,
		CloserChan:  closerChan,
		TempDir:     tempDir,
		ConsumeResp: consumeResp,
		Processors: map[ProcessingOperation]FFmpegProcessor{
			OperationCompress:      NewVideoCompressionProcessor(tempDir),
			OperationSegment:       NewVideoSegmentProcessor(tempDir),
			OperationExtractFrames: NewFrameExtractionProcessor(tempDir),
		},
	}
}

func (bvw *BatchVideoWorker) Work(ctx context.Context, jobQueueChan <-chan *VideoBatchedJob) {
	defer func() {
		bvw.CloserChan <- bvw.Idx
	}()

	for {
		select {
		case <-ctx.Done():
			log.Printf("BatchVideoWorker %d exiting...", bvw.Idx)
			return
		case batch := <-jobQueueChan:
			log.Printf("BatchVideoWorker %d processing batch: %s with %d jobs", bvw.Idx, batch.UID, len(batch.Jobs))

			for _, job := range batch.Jobs {
				_, exists := bvw.Processors[job.Operation]
				if !exists {
					log.Printf("Unknown operation: %s", job.Operation)
					if bvw.ConsumeResp {
						job.MailBox <- &VideoProcessingResult{
							VideoID: job.ID,
							Status:  "failed",
							Error:   fmt.Sprintf("Unknown operation: %s", job.Operation),
						}
					}
					close(job.Done)
					continue
				}

				// Convert VideoJob to mediahose.Job for polymorphic processing
				mediaJob := &mediahose.Job{
					ID:              job.ID,
					ImagePath:       job.VideoPath,
					Format:          "mp4",
					Quality:         job.Quality,
					MediaType:       mediahose.MediaTypeVideo,
					Resp:            job.Resp,
					Done:            job.Done,
					MailBox:         job.MailBox,
					Ctx:             job.Ctx,
					CancelCtx:       job.CancelCtx,
					Encoder:         job.Encoder,
					DefaultS3Bucket: job.DefaultS3Bucket,
					SkipResize:      true,
					SkipUpload:      job.SkipUpload,
					Metadata:        make(map[string]interface{}),
				}

				// Set operation in metadata
				mediaJob.Metadata["operation"] = string(job.Operation)
				if job.Config != nil {
					mediaJob.Metadata["config"] = job.Config
				}

				jobProcessor := NewVideoJobProcessor(string(job.Operation), bvw.TempDir)
				
				if err := jobProcessor.Process(ctx, mediaJob); err != nil {
					log.Printf("Batch video processing failed for job %s: %v", job.ID, err)
					if bvw.ConsumeResp {
						job.MailBox <- &VideoProcessingResult{
							VideoID: job.ID,
							Status:  "failed",
							Error:   err.Error(),
							Metadata: map[string]interface{}{
								"error_details": err.Error(),
							},
						}
					}
					close(job.Done)
					continue
				}

				if bvw.ConsumeResp {
					job.MailBox <- &VideoProcessingResult{
						VideoID:   job.ID,
						Status:    "completed",
						Operation: string(job.Operation),
					}
				}

				log.Printf("BatchVideoWorker %d completed job: %s", bvw.Idx, job.ID)
				close(job.Done)
			}
		}
	}
}

// VideoBatchedJob represents a batch of video processing jobs
type VideoBatchedJob struct {
	UID        string
	Jobs       []*VideoJob
	BatchSize  int32
	Processing bool
	Retries    int32
	CreatedAt  time.Time
}

// VideoProcessingResult represents the result of video processing for batch jobs
type VideoProcessingResult struct {
	VideoID   string                 `json:"video_id"`
	Status    string                 `json:"status"`
	Operation string                 `json:"operation,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// VideoWorkerFactory creates video workers for the dynamic scaler
func VideoWorkerFactory(tempDir string) func(idx int64, done chan int64) mediahose.Worker[*VideoJob] {
	return func(idx int64, done chan int64) mediahose.Worker[*VideoJob] {
		return NewVideoWorker(idx, done, tempDir)
	}
}

// BatchVideoWorkerFactory creates batch video workers for the dynamic scaler
func BatchVideoWorkerFactory(tempDir string, consumeResp bool) func(idx int64, done chan int64) mediahose.Worker[*VideoBatchedJob] {
	return func(idx int64, done chan int64) mediahose.Worker[*VideoBatchedJob] {
		return NewBatchVideoWorker(idx, done, tempDir, consumeResp)
	}
}
