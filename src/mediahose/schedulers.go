package mediahose

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/services/mediametadata"
	"github.com/go-batteries/optimux/src/shared"
)

type Worker[T any] interface {
	Work(ctx context.Context, jobQueueCh <-chan T)
}

// RetireAwareWorker lets a worker opt into retire-at-idle handshaking.
// Workers that don't implement this fall back to legacy ctx-cancel retirement.
type RetireAwareWorker interface {
	SetRetireCh(chan chan bool)
}

// FetchWorker downloads images and processes them
type FetchWorker struct {
	Idx        int64
	CloserChan chan int64
	RetireCh   chan chan bool
}

func (fw *FetchWorker) SetRetireCh(ch chan chan bool) { fw.RetireCh = ch }

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
		case retireReq := <-fw.RetireCh:
			// Idle boundary: we only get here when not mid-job.
			// Ack via send, never close (scaler owns the ack channel).
			select {
			case retireReq <- true:
			default:
			}
			log.Println("FetchWorker retiring at idle boundary...")
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

// WorkerSlot tracks one live worker in creation order.
// Tail of DynamicScaler.workers is the newest slot (LIFO retire target).
type WorkerSlot[T any] struct {
	Idx      int64
	Retiring bool // marked for retirement; reclaimed only on exit notify

	worker Worker[T]
	cancel context.CancelFunc
	retire chan chan bool // created & owned by the scaler; never closed
}

// DynamicScaler manages workers dynamically
type DynamicScaler[T any] struct {
	WorkerFactory      func(idx int64, done chan int64) Worker[T]
	Queue              chan T
	MinWorkers         int
	MaxWorkers         int
	ScaleUpThreshold   int
	ScaleDownThreshold int
	ScaleSigChan       chan struct{}
	Name               string

	CheckInterval time.Duration // scale() cadence; default 5s
	ScaleCooldown time.Duration // min time between scale-up/down; default 30s
	RetireGrace   time.Duration // wait before cancel-nudge; default 2s

	mu              sync.RWMutex
	workers         []*WorkerSlot[T] // creation order → tail = newest
	nextIdx         int64
	lastScale       time.Time
	workerCloseChan chan int64
}

func BootStrapDynamicScalerFrom[T any](scaler *DynamicScaler[T]) *DynamicScaler[T] {
	buf := scaler.MaxWorkers
	if buf < 1 {
		buf = 1
	}
	if scaler.MinWorkers > buf {
		buf = scaler.MinWorkers
	}
	scaler.workerCloseChan = make(chan int64, buf)

	if scaler.CheckInterval <= 0 {
		scaler.CheckInterval = 5 * time.Second
	}
	if scaler.ScaleCooldown < 0 {
		scaler.ScaleCooldown = 0 // explicit disable
	} else if scaler.ScaleCooldown == 0 {
		scaler.ScaleCooldown = 30 * time.Second // safe default
	}
	if scaler.RetireGrace <= 0 {
		scaler.RetireGrace = 2 * time.Second
	}

	return scaler
}

func countLive[T any](workers []*WorkerSlot[T]) int {
	live := 0
	for _, wsl := range workers {
		if !wsl.Retiring {
			live++
		}
	}
	return live
}

// ActiveCount returns the current number of live (non-retiring) workers.
// It is safe for concurrent reads, unlike a raw counter field.
func (ds *DynamicScaler[T]) ActiveCount() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return countLive(ds.workers)
}

// Start monitoring and scaling workers
func (ds *DynamicScaler[T]) Start(ctx context.Context) {
	log.Println("starting auto scaled workers", ds.Name)

	ds.mu.Lock()
	for i := 0; i < ds.MinWorkers; i++ {
		ds.addWorkerLocked(ctx)
	}
	ds.mu.Unlock()

	go func() {
		ticker := time.NewTicker(ds.CheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("shutting down")
				ds.shutdown()
				return
			case idx := <-ds.workerCloseChan:
				ds.removeWorkerByIdx(idx)
				ds.scale(ctx) // floor bypass handles crash refill
			case <-ds.ScaleSigChan:
				ds.scale(ctx)
			case <-ticker.C:
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
	live := countLive(ds.workers)
	total := len(ds.workers)

	ins := shared.I()
	tags := shared.GetServerTags()

	ins.Gauge(shared.EventOnTheFlyQLen, float64(queueLen), tags, 1)
	ins.Gauge(shared.EventScalerWorkerCount, float64(live), tags, 1)

	// Floor recovery always allowed — do not leave the pool empty after crashes.
	belowFloor := live < ds.MinWorkers && total < ds.MaxWorkers

	if !belowFloor && ds.ScaleCooldown > 0 && time.Since(ds.lastScale) < ds.ScaleCooldown {
		return
	}

	switch {
	case queueLen > ds.ScaleUpThreshold && total < ds.MaxWorkers:
		log.Printf("🌀 Scaling up worker for %s...", ds.Name)
		ds.addWorkerLocked(ctx)
		ds.lastScale = time.Now()

		ins.Incr(shared.EventScalerScaleUp, tags, 1)
	case queueLen < ds.ScaleDownThreshold && live > ds.MinWorkers:
		log.Printf("🔴 Scaling down worker for %s...", ds.Name)
		ds.retireWorkerLocked()
		ds.lastScale = time.Now()

		ins.Incr(shared.EventScalerScaleDown, tags, 1)
	case belowFloor:
		// workers have kept dying, yet queue < threshold
		log.Printf("🌀 Scaling up worker for %s...", ds.Name)
		ds.addWorkerLocked(ctx)
		ds.lastScale = time.Now()

		ins.Incr(shared.EventScalerScaleUp, tags, 1)
	}
}

// Adds a new worker with a cancelable context. Caller must hold ds.mu.
func (ds *DynamicScaler[T]) addWorkerLocked(ctx context.Context) {
	worker := ds.WorkerFactory(ds.nextIdx, ds.workerCloseChan)
	workerCtx, cancel := context.WithCancel(ctx)
	retire := make(chan chan bool, 1) // scaler-owned; never closed

	if rw, ok := worker.(RetireAwareWorker); ok {
		rw.SetRetireCh(retire)
	}

	wsl := &WorkerSlot[T]{
		Idx: ds.nextIdx, worker: worker, cancel: cancel, retire: retire,
	}
	ds.workers = append(ds.workers, wsl)
	ds.nextIdx++
	go worker.Work(workerCtx, ds.Queue)
}

// newestNonRetiring scans tail → head for the LIFO retire target. Caller must hold ds.mu.
func (ds *DynamicScaler[T]) newestNonRetiring() *WorkerSlot[T] {
	for i := len(ds.workers) - 1; i >= 0; i-- {
		if !ds.workers[i].Retiring {
			return ds.workers[i]
		}
	}
	return nil
}

// retireWorkerLocked marks the newest live worker for retirement and requests
// it retire at its next idle boundary. Caller must hold ds.mu.
func (ds *DynamicScaler[T]) retireWorkerLocked() {
	wsl := ds.newestNonRetiring()
	if wsl == nil {
		return
	}
	wsl.Retiring = true
	log.Println(ds.Name, "retiring worker", wsl.Idx)

	if _, ok := wsl.worker.(RetireAwareWorker); !ok || wsl.retire == nil {
		wsl.cancel() // legacy: ctx-cancel retire
		return
	}

	ack := make(chan bool, 1)
	select {
	case wsl.retire <- ack:
		// Request queued. Worker will ack when it next parks idle.
	default:
		// Buffer full (should not happen: one retire per slot). Nudge.
		wsl.cancel()
		return
	}

	grace := ds.RetireGrace
	go func() {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-ack:
			log.Println(ds.Name, "worker", wsl.Idx, "acked retirement")
			// Do not close(ack): worker may race a late send; GC is enough
			// for a 1-buffer chan that nobody else receives from.
		case <-timer.C:
			// Still mid-job after grace. Cancel is a *nudge*, not the reclaim.
			// Reclaim still happens only on CloserChan exit notify.
			wsl.cancel()
		}
	}()
}

// findSlot returns the slot for idx, or nil. Caller must hold ds.mu.
func (ds *DynamicScaler[T]) findSlot(idx int64) *WorkerSlot[T] {
	for _, wsl := range ds.workers {
		if wsl.Idx == idx {
			return wsl
		}
	}
	return nil
}

// dropSlot removes the slot for idx from the registry. Caller must hold ds.mu.
func (ds *DynamicScaler[T]) dropSlot(idx int64) {
	for i, wsl := range ds.workers {
		if wsl.Idx == idx {
			ds.workers = append(ds.workers[:i], ds.workers[i+1:]...)
			return
		}
	}
}

// removeWorkerByIdx reclaims a worker's slot on its exit notification.
// This is the single source of truth for registry removal.
func (ds *DynamicScaler[T]) removeWorkerByIdx(idx int64) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if wsl := ds.findSlot(idx); wsl != nil {
		ds.dropSlot(idx)
		log.Println(ds.Name, "worker", wsl.Idx, "exited; reclaimed")
	}
}

// Gracefully shut down all workers
func (ds *DynamicScaler[T]) shutdown() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	for _, wsl := range ds.workers {
		wsl.cancel()
	}
	ds.workers = nil
	// do not close workerCloseChan or per-slot retire chans
}
