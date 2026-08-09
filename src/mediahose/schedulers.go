package mediahose

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/roverxio/optimux/src/encoders"
	"github.com/roverxio/optimux/src/services/mediametadata"
	"github.com/roverxio/optimux/src/shared"
)

type Worker[T any] interface {
	Work(ctx context.Context, jobQueueCh <-chan T)
}

// FetchWorker downloads images and processes them
type FetchWorker struct {
	Idx        int64
	CloserChan chan int64
}

// Process Media
func (fw *FetchWorker) Work(ctx context.Context, jobQueueChan <-chan *Job) {
	defer func() {
		fw.CloserChan <- fw.Idx
	}()

	for {
		select {
		case <-ctx.Done():
			log.Println("FetchWorker exiting...")
			return
		case job := <-jobQueueChan:
			log.Println("Fetching media:", job.MediaType, job.ImagePath, "quality", job.Quality, "format", job.Format)

			var err error
			var processor MediaProcessor

			if job.MediaType == MediaTypeImage {
				processor = &ImageProcessor{ImageLoader: job.ImageLoader} // todo: use the strategy pattern here
			} else if job.MediaType == MediaTypeVideo {
				// Use FFmpeg processor for video processing (sprites, transcoding, etc.)
				processor = &VideoFFmpegProcessor{
					S3Bucket: job.DefaultS3Bucket,
				}
			}

			byts, err := processor.Process(ctx, job)
			if err != nil {
				log.Printf("processing failed. reason %v", err)

				job.ErrHandler(job.Resp, "processing failed", http.StatusInternalServerError)

				close(job.Done)
				continue // Process next job
			}

			log.Printf("SCHEDULER: Got %d bytes from processor for job %s, MediaType=%v", len(byts), job.ID, job.MediaType)
			log.Printf("SCHEDULER: job.Encoder=%v, job.Resp=%v", job.Encoder != nil, job.Resp != nil)

			if job.MediaType == MediaTypeImage {
			size := job.Sizes[0]
			key := fmt.Sprintf("%d_%d_%d.%s", size[0], size[1], job.Quality, job.Format)

			log.Printf("SCHEDULER: Calling image encoder with %d bytes", len(byts))
			job.Encoder(ctx, &encoders.ResponseOpts{
				Filename: key, Format: job.Format, Data: byts,
			}, job.Resp)

			// job.Encoder(byts, key, job.Format, job.Resp)
		} else {
			log.Printf("SCHEDULER: Calling video encoder with %d bytes, filename=%s, format=%s", len(byts), job.ImagePath, job.Format)
			job.Encoder(ctx, &encoders.ResponseOpts{
				Filename: job.ImagePath, Format: job.Format, Data: byts,
			}, job.Resp)

			// job.Encoder(byts, job.ImagePath, job.Format, job.Resp)
		}

			// Mark for GC
			byts = nil

			// Signal processing complete
			close(job.Done)
		}
	}
}

type BatchWorker struct {
	Idx         int64
	CloserChan  chan int64
	ConsumeResp bool
}

func (bw *BatchWorker) Work(ctx context.Context, jobQueueChan <-chan *BatchedJob) {
	defer func() {
		bw.CloserChan <- bw.Idx
	}()

	for {
		select {
		case <-ctx.Done():
			log.Println("BatchWorker exiting...")
			return
		case batch := <-jobQueueChan:
			log.Printf("Processing batch UID: %s, %d images\n", batch.UID, len(batch.Jobs))

			// Enqueue jobs to `fetchQueue` for existing fetch workers
			for _, job := range batch.Jobs {
				log.Println("Fetching media from batch:", job.MediaType, job.ImagePath, "quality", job.Quality)

				var err error
				var processor MediaProcessor

				mediaSchema := &mediametadata.V1MedatataSchema{
					OriginalPath: job.ImagePath,
					Sizes:        []*mediametadata.SizeFormatTuple{},
					Bucket:       job.DefaultS3Bucket,
				}

				fileName, ext, ok := shared.ExplodeFileName(*job.S3Key)
				if !ok {
					log.Println("failed to get filename and ext from s3key", *job.S3Key)
					close(job.Done)

					continue
				}

				log.Println("job info", job.ImagePath, job.Sizes, job.Done)
				log.Println("image format requested", job.Format, "original extension", ext)

				sizeID := shared.ExtractSizeIDFromFile(fileName)

				if sizeID == "" {
					if bw.ConsumeResp {
						job.MailBox <- &mediametadata.MediaMetadata{
							MediaID:        job.ID,
							Source:         "s3",
							Version:        "v1",
							Status:         shared.ToPtr(mediametadata.StateProcessingFailed),
							MetadataSchema: mediaSchema,
							CreatedAt:      time.Now().UTC(),
							ProcessedAt:    time.Now().UTC(),
						}
					}

					close(job.Done)

					log.Println("sizeID is not present in fileName")
					continue
				}

				// TODO: replace  job.Processor instead of Loaders
				if job.MediaType == MediaTypeImage {
					processor = &ImageProcessor{ImageLoader: job.ImageLoader}
				} else if job.MediaType == MediaTypeVideo {
					processor = &VideoProcessor{Client: DefaultS3HTTPClient()}
				}

				byts, err := processor.Process(ctx, job)
				if err != nil {
					log.Printf("processing failed. reason %v", err)
					job.ErrHandler(job.Resp, "processing failed", http.StatusInternalServerError)

					if bw.ConsumeResp {
						job.MailBox <- &mediametadata.MediaMetadata{
							MediaID:        job.ID,
							Source:         "s3",
							Version:        "v1",
							Status:         shared.ToPtr(mediametadata.StateProcessingFailed),
							MetadataSchema: mediaSchema,
							CreatedAt:      time.Now().UTC(),
							ProcessedAt:    time.Now().UTC(),
						}
					}

					close(job.Done)
					continue
				}

				if job.MediaType == MediaTypeImage {
					size := job.Sizes[0]
					key := fmt.Sprintf("%d_%d_%d.%s", size[0], size[1], job.Quality, job.Format)

					job.Encoder(ctx, &encoders.ResponseOpts{
						Filename:   key,
						Format:     job.Format,
						Data:       byts,
						S3Key:      job.S3Key,
						S3Bucket:   job.S3Bucket,
						SkipUpload: job.SkipUpload,
					}, job.Resp)

					// job.Encoder(byts, key, job.Format, job.Resp)
				} else {
					job.Encoder(ctx, &encoders.ResponseOpts{
						Filename:   job.ImagePath,
						Format:     job.Format,
						Data:       byts,
						S3Bucket:   job.S3Bucket,
						S3Key:      job.S3Key,
						SkipUpload: job.SkipUpload,
					}, job.Resp)

					// job.Encoder(byts, job.ImagePath, job.Format, job.Resp)
				}

				mediaSchema.Sizes = []*mediametadata.SizeFormatTuple{{Size: sizeID, Format: job.Format}}

				// b, err := json.Marshal(mediaSchema)
				// if err != nil {
				// 	close(job.Done)
				// 	log.Printf("failed to marshal metadata. error %v", err)
				// 	continue
				// }

				if bw.ConsumeResp {
					if job.S3Bucket != nil { // this should always be true for energon worker.
						mediaSchema.Bucket = *job.S3Bucket
					}

					response := &mediametadata.MediaMetadata{
						MediaID:        job.ID,
						Source:         "s3",
						Version:        "v1",
						Status:         shared.ToPtr(mediametadata.StateProcessed),
						MetadataSchema: mediaSchema,
						CreatedAt:      time.Now().UTC(),
						ProcessedAt:    time.Now().UTC(),
					}

					// log.Println("sending for", job.ID, job.Done, job.Sizes, *response)

					select {
					case job.MailBox <- response:
						log.Println("✅ sent value", job.ID, job.Done, response)
					default:
						log.Println("⚠️ send skipped - no receiver?", job.ID, job.Done)
					}
				}

				byts = nil

				close(job.Done)
			}
		}
	}
}

// DynamicScaler manages workers dynamically
type DynamicScaler[T any] struct {
	WorkerFactory      func(idx int64, done chan int64) Worker[T]
	Queue              chan T
	MinWorkers         int
	MaxWorkers         int
	ScaleUpThreshold   int
	ScaleDownThreshold int
	mu                 sync.RWMutex
	ActiveWorkers      int
	nextIdx            int64
	cancelFuncs        map[int64]context.CancelFunc
	workerCloseChan    chan int64
	ScaleSigChan       chan struct{}
	Name               string
}

func BootStrapDynamicScalerFrom[T any](scaler *DynamicScaler[T]) *DynamicScaler[T] {
	scaler.cancelFuncs = make(map[int64]context.CancelFunc)
	scaler.workerCloseChan = make(chan int64, 1)

	return scaler
}

// Start monitoring and scaling workers
func (ds *DynamicScaler[T]) Start(ctx context.Context) {
	log.Println("starting auto scaled workers", ds.Name)

	for i := 0; i < ds.MinWorkers; i++ {
		ds.addWorker(ctx)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Println("shutting down")
				ds.shutdown()
				return
			case idx := <-ds.workerCloseChan:
				ds.removeWorkerByIdx(idx)
				ds.scale(ctx)
			case <-ds.ScaleSigChan:
				ds.scale(ctx)
			case <-time.After(5 * time.Second):
				ds.scale(ctx)
			}
		}
	}()
}

// Scaling logic
func (ds *DynamicScaler[T]) scale(ctx context.Context) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	queueLen := len(ds.Queue)

	ins := shared.I()
	tags := shared.GetServerTags()

	ins.Gauge(shared.EventOnTheFlyQLen, float64(queueLen), tags, 1)
	ins.Gauge(shared.EventScalerWorkerCount, float64(ds.ActiveWorkers), tags, 1)

	// changed := false
	// log.Println("checking if workers need to scale", ds.Name, queueLen, ds.ScaleUpThreshold,
	// 	ds.ScaleDownThreshold, ds.ActiveWorkers, ds.MaxWorkers)

	if queueLen > ds.ScaleUpThreshold && ds.ActiveWorkers < ds.MaxWorkers {
		log.Printf("🌀 Scaling up worker for %s...", ds.Name)
		ds.addWorker(ctx)

		ins.Incr(shared.EventScalerScaleUp, tags, 1)
		return
	}

	if queueLen < ds.ScaleDownThreshold && ds.ActiveWorkers > ds.MinWorkers {
		log.Printf("🔴 Scaling down worker for %s...", ds.Name)
		ds.removeWorker()

		ins.Incr(shared.EventScalerScaleDown, tags, 1)
		return
	}

	if queueLen < ds.ScaleUpThreshold && ds.ActiveWorkers < ds.MinWorkers {
		// workers have kept dying, yet queue < threshold
		log.Printf("🌀 Scaling up worker for %s...", ds.Name)
		ds.addWorker(ctx)

		ins.Incr(shared.EventScalerScaleUp, tags, 1)
	}

	// log.Println("changed?", changed)
}

// Adds a new worker with a cancelable context
func (ds *DynamicScaler[T]) addWorker(ctx context.Context) {
	worker := ds.WorkerFactory(ds.nextIdx, ds.workerCloseChan)

	workerCtx, cancel := context.WithCancel(ctx)
	ds.cancelFuncs[ds.nextIdx] = cancel

	go worker.Work(workerCtx, ds.Queue)

	ds.nextIdx++
	ds.ActiveWorkers++
}

// Removes a worker gracefully
func (ds *DynamicScaler[T]) removeWorker() {
	if ds.ActiveWorkers <= ds.MinWorkers {
		log.Println("Cannot scale down below minimum workers", ds.Name)
		return
	}

	// Select the first available worker to terminate
	for idx, cancel := range ds.cancelFuncs {
		cancel() // Cancel the worker context
		delete(ds.cancelFuncs, idx)
		ds.ActiveWorkers--
		log.Println(ds.Name, "Worker removed:", idx, "Remaining Active Workers:", ds.ActiveWorkers)
		return
	}
}

// Remove worker by index
func (ds *DynamicScaler[T]) removeWorkerByIdx(idx int64) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	log.Println(ds.Name, "Worker idx", idx, "died. respawning")

	if idx > ds.nextIdx {
		// not possible
		return
	}

	if _, ok := ds.cancelFuncs[idx]; !ok {
		// worker has already been removed
		return
	}

	log.Println("removing workers by id", ds.Name, ds.ActiveWorkers, ds.MinWorkers)

	ds.ActiveWorkers--
	ds.cancelFuncs[idx]()

	// log.Println("now active workers", ds.Name, ds.ActiveWorkers)
	delete(ds.cancelFuncs, idx)
}

// Gracefully shut down all workers
func (ds *DynamicScaler[T]) shutdown() {
	for _, cancel := range ds.cancelFuncs {
		cancel()
	}
	ds.ActiveWorkers = 0
	ds.cancelFuncs = nil
}
