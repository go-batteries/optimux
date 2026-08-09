package mediahose

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/roverxio/optimux/src/encoders"
	"github.com/roverxio/optimux/src/shared"
)

type EventCallback func(ctx context.Context, data any) error

type Emitter interface {
	Emit(ctx context.Context, event string, args any) chan struct{}
	On(event string, callbacks ...EventCallback) error
}

type EventEmitter struct {
	mu       sync.RWMutex
	eventMap map[string][]EventCallback
}

func NewEventEmitter() *EventEmitter {
	return &EventEmitter{
		eventMap: make(map[string][]EventCallback),
	}
}

func (self *EventEmitter) On(event string, callbacks ...EventCallback) error {
	self.mu.Lock()
	defer self.mu.Unlock()

	exists, ok := self.eventMap[event]
	if !ok {
		exists = []EventCallback{}
	}

	exists = append(exists, callbacks...)
	self.eventMap[event] = exists

	return nil
}

func (self *EventEmitter) Emit(ctx context.Context, event string, args any) chan struct{} {
	self.mu.RLock()
	defer self.mu.RUnlock()

	done := make(chan struct{})

	callbacks, ok := self.eventMap[event]
	if !ok {
		close(done)
		return done
	}

	go func(callbacks []EventCallback) {
		defer close(done)

		var wg sync.WaitGroup
		for _, callback := range callbacks {
			wg.Add(1)

			go func(cb EventCallback) {
				defer wg.Done()
				cb(ctx, args)
			}(callback)
		}

		wg.Wait()
	}(callbacks)

	return done
}

type MediaType int8

const (
	MediaTypeImage MediaType = 1
	MediaTypeVideo MediaType = 2
)

// Job represents a media processing task (images, videos, etc.)
type Job struct {
	ID              string
	ImagePath       string // Also used for video path
	Format          string   // "jpeg" or "webp" or "png" or "mp4" etc
	Sizes           [][2]int // List of width-height pairs
	Quality         int
	MediaType       MediaType
	Resp            http.ResponseWriter
	Done            shared.DoneCh
	MailBox         shared.MailBoxCh
	Ctx             context.Context
	CancelCtx       context.CancelFunc
	Encoder         encoders.Encoder
	ErrHandler      func(w io.Writer, msg string, code int)
	ImageLoader     LoadImageStrategy
	OrigPath        string
	DefaultS3Bucket string

	ConvertToFormat *string
	S3Bucket        *string
	S3Key           *string

	// Convert to int32
	SkipResize bool
	SkipUpload bool
	
	// Generic metadata for extensibility
	Metadata map[string]interface{}
	
	// Processor for polymorphic processing
	processor JobProcessor
}


// BatchedJob for batching image processing requests
type BatchedJob struct {
	UID        string
	Jobs       []*Job
	Timer      *time.Timer
	BatchSize  int32
	Processing bool
	Retries    int32
	CreatedAt  time.Time
}

const (
	DefaultBatchWorkerTimeout = 45 * time.Second
)

type BatchResponse struct {
	UID        string
	Retries    int32
	Processing bool
}

func BatchDeleteTmpFiles(ctx context.Context, args any) error {
	var errList []error
	jobs, ok := args.([]*Job)

	if !ok || len(jobs) == 0 {
		return errors.New("all args were invalid")
	}

	for _, job := range jobs {
		cachedFile := shared.GetCacheFilePath(job.ImagePath)

		log.Println("deleting", cachedFile)

		if _, err := os.Stat(cachedFile); err == nil {
			if err := os.Remove(cachedFile); err != nil {
				errList = append(errList, fmt.Errorf("failed to delete %s: %w", cachedFile, err))
			}
		} else if !os.IsNotExist(err) {
			log.Printf("file not found. stat error on %s: %v", cachedFile, err)
		}
	}

	if len(errList) > 0 {
		return fmt.Errorf("batch delete errors: %v", errList)
	}

	log.Println("all files deleted succesfully")
	return nil
}

func NewBatchedEventEmitter() *EventEmitter {
	e := NewEventEmitter()

	e.On("process::done", BatchDeleteTmpFiles)
	return e
}

// Dispatcher manages batching of requests
type Dispatcher struct {
	mu             sync.Mutex
	batches        map[string]*BatchedJob
	interval       time.Duration
	queue          chan *BatchedJob
	onPushComplete func(batch *BatchedJob)

	Events Emitter
}

func OnBatchEnqueueComplete(batch *BatchedJob) {
	for _, job := range batch.Jobs {
		err := shared.FlushResponse(job.Resp, func(w http.ResponseWriter) bool {
			u, err := url.Parse(job.OrigPath)
			if err != nil {
				log.Println("failed to parse image path", job.OrigPath)
				return false
			}

			w.Header().Add("Link", shared.BuildLinkHeader(u))
			return true
		})
		if err != nil {
			log.Printf("failed to flush link headers, http2 preload won't work. %v", err)
		}
	}

	log.Println("link headers sent for batch", batch.UID)
}

func NoOpOnComplete(batch *BatchedJob) {
	return
}

func NewDispatcher(interval time.Duration, queueChan chan *BatchedJob, onPushComplete func(*BatchedJob)) *Dispatcher {
	return &Dispatcher{
		batches:        make(map[string]*BatchedJob),
		interval:       interval,
		queue:          queueChan,
		onPushComplete: onPushComplete,
		Events:         NewBatchedEventEmitter(),
	}
}

func (d *Dispatcher) RunInBackground(ctx context.Context) chan bool {
	done := make(chan bool)

	go func() {
		ticker := time.NewTicker(d.interval)

		defer ticker.Stop()
		defer func() { close(done) }()

		for {
			select {
			case <-ticker.C:
				d.ProcessPendingBatches(ctx)
			case <-ctx.Done():
				log.Println("shutting down, since context cancelled")
				return
			}
		}
	}()

	return done
}

// Add request to a batch (by UID)
func (d *Dispatcher) Add(ctx context.Context, uid string, job *Job) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// log.Println("adding job to batch")

	batch, exists := d.batches[uid]
	if !exists {
		batch = &BatchedJob{UID: uid, Jobs: []*Job{job}, BatchSize: 1, CreatedAt: time.Now().UTC()}
		d.batches[uid] = batch
	} else {
		batch.Jobs = append(batch.Jobs, job)
		batch.BatchSize = int32(len(batch.Jobs))
	}
}

// Process all pending batches (called every `interval`)
func (d *Dispatcher) ProcessPendingBatches(ctx context.Context) {
	// defer Bench("Process pending batch jobs")()

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, batch := range d.batches {
		if len(batch.Jobs) > 0 {
			log.Printf("🚀 Processing pending batch %s with %d jobs\n", batch.UID, len(batch.Jobs))

			resp, ok := d.ProcessBatch(ctx, batch)
			// log.Println("batch processing response", resp, ok)

			if !ok {
				// Keep the batch for re-processing
				batch.Processing = resp.Processing
				batch.Retries = resp.Retries

				continue
			}

			log.Println("deleting jobs ", len(d.batches), batch.UID)

			<-d.Events.Emit(ctx, "process::done", batch.Jobs)
			delete(d.batches, batch.UID)
		}
	}
}

// ProcessBatch processes all jobs under the given UID
func (d *Dispatcher) ProcessBatch(ctx context.Context, batch *BatchedJob) (*BatchResponse, bool) {
	defer shared.Bench(fmt.Sprintf("processing batch %s", batch.UID))()

	log.Printf("🚀 Processing batch %s with %d jobs\n", batch.UID, len(batch.Jobs))

	// d.mu.Lock()         // Lock_1
	// defer d.mu.Unlock() // Un Lock_1
	resp := &BatchResponse{Processing: true, Retries: batch.Retries, UID: batch.UID}

	// batch.Processing = true
	// delete(d.batches, batch.UID)

	// jobDoneMap := map[*Job]struct{}{}

	uid := batch.UID

	select {
	case d.queue <- batch:
		log.Printf("📥 Submitted batch %s with %d requests\n", uid, batch.BatchSize)

		d.onPushComplete(batch)

	case <-ctx.Done():
		log.Println("early, returning from process batch")
		return nil, false

	case <-time.After(DefaultBatchWorkerTimeout):
		log.Println("Timing out waiting to enqueue batch", uid)

		resp.Processing = false

		if batch.Retries == 2 {
			log.Println("Batch queue full, dropping batch:", uid)

			resp.Retries = -1
			return resp, true
		}

		log.Println("re-enqueuing batch", uid, "retries yet", batch.Retries)

		resp.Retries += 1
		// d.batches[uid] = batch

		return resp, false
	}

	log.Println("waiting for jobs to complete, for batch", uid)
	now := time.Now()

	var wg sync.WaitGroup
	wg.Add(len(batch.Jobs))

	for _, job := range batch.Jobs {
		go func(j *Job) {
			defer wg.Done()

			select {
			case <-j.Done:
			// jobDoneMap[job] = struct{}{}
			case <-time.After(DefaultBatchWorkerTimeout):
				// TODO:  will need to re-enqueue.
				log.Println("timed out trying to process batch, dropping and cancelling")
				j.CancelCtx()

			case <-ctx.Done():
				log.Println("exiting for malaria")
			}
		}(job)
	}

	log.Println("Waiting for jobs to end")
	wg.Wait()

	// resp.Processing = false
	// resp.Retries = 0

	// restJobs := []*Job{}
	//
	// // Remove the jobs that are conpleted
	// for _, job := range batch.Jobs {
	// 	if _, ok := jobDoneMap[job]; !ok {
	// 		restJobs = append(restJobs, job)
	// 	}
	// }
	//
	// batch.Jobs = restJobs

	log.Println("all jobs completed for batch", uid, ", in", time.Since(now))
	return resp, true
}
