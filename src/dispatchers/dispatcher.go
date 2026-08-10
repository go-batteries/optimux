package dispatchers

//
// import (
// 	"context"
// 	"fmt"
// 	"log"
// 	"sync"
// 	"time"
//
// 	"github.com/go-batteries/optimux/src/mediahose"
// 	"github.com/go-batteries/optimux/src/shared"
// )
//
// // Dispatcher manages batching of requests
// type Dispatcher struct {
// 	mu             sync.Mutex
// 	batches        map[string]*mediahose.BatchedJob
// 	interval       time.Duration
// 	queue          chan *mediahose.BatchedJob
// 	onPushComplete func(batch *mediahose.BatchedJob)
// }
//
// func NewDispatcher(interval time.Duration, queueChan chan *mediahose.BatchedJob, onPushComplete func(*mediahose.BatchedJob)) *Dispatcher {
// 	return &Dispatcher{
// 		batches:        make(map[string]*mediahose.BatchedJob),
// 		interval:       interval,
// 		queue:          queueChan,
// 		onPushComplete: onPushComplete,
// 	}
// }
//
// func (d *Dispatcher) RunInBackground(ctx context.Context) chan bool {
// 	done := make(chan bool)
//
// 	go func() {
// 		ticker := time.NewTicker(d.interval)
//
// 		defer ticker.Stop()
// 		defer func() { close(done) }()
//
// 		for {
// 			select {
// 			case <-ticker.C:
// 				d.ProcessPendingBatches(ctx)
// 			case <-ctx.Done():
// 				log.Println("shutting down, since context cancelled")
// 				return
// 			}
// 		}
// 	}()
//
// 	return done
// }
//
// // Add request to a batch (by UID)
// func (d *Dispatcher) Add(ctx context.Context, uid string, job *mediahose.Job) {
// 	d.mu.Lock()
// 	defer d.mu.Unlock()
//
// 	// log.Println("adding job to batch")
//
// 	batch, exists := d.batches[uid]
// 	if !exists {
// 		batch = &mediahose.BatchedJob{
// 			UID:       uid,
// 			Jobs:      []*mediahose.Job{job},
// 			BatchSize: 1,
// 			CreatedAt: time.Now().UTC(),
// 		}
//
// 		d.batches[uid] = batch
// 	} else {
// 		batch.Jobs = append(batch.Jobs, job)
// 		batch.BatchSize = int32(len(batch.Jobs))
// 	}
// }
//
// // Process all pending batches (called every `interval`)
// func (d *Dispatcher) ProcessPendingBatches(ctx context.Context) {
// 	// defer Bench("Process pending batch jobs")()
//
// 	d.mu.Lock()
// 	defer d.mu.Unlock()
//
// 	for _, batch := range d.batches {
// 		if len(batch.Jobs) > 0 {
// 			log.Printf("🚀 Processing pending batch %s with %d jobs\n", batch.UID, len(batch.Jobs))
//
// 			resp, ok := d.ProcessBatch(ctx, batch)
// 			// log.Println("batch processing response", resp, ok)
//
// 			if !ok {
// 				// Keep the batch for re-processing
// 				batch.Processing = resp.Processing
// 				batch.Retries = resp.Retries
//
// 				continue
// 			}
//
// 			log.Println("deleting jobs ", len(d.batches), batch.UID)
//
// 			delete(d.batches, batch.UID)
// 		}
// 	}
// }
//
// const (
// 	DefaultBatchWorkerTimeout = 45 * time.Second
// )
//
// type BatchResponse struct {
// 	UID        string
// 	Retries    int32
// 	Processing bool
// }
//
// // ProcessBatch processes all jobs under the given UID
// func (d *Dispatcher) ProcessBatch(ctx context.Context, batch *mediahose.BatchedJob) (*BatchResponse, bool) {
// 	defer shared.Bench(fmt.Sprintf("processing batch %s", batch.UID))()
//
// 	log.Printf("🚀 Processing batch %s with %d jobs\n", batch.UID, len(batch.Jobs))
//
// 	// d.mu.Lock()         // Lock_1
// 	// defer d.mu.Unlock() // Un Lock_1
// 	resp := &BatchResponse{Processing: true, Retries: batch.Retries, UID: batch.UID}
//
// 	// batch.Processing = true
// 	// delete(d.batches, batch.UID)
//
// 	// jobDoneMap := map[*Job]struct{}{}
//
// 	uid := batch.UID
//
// 	select {
// 	case d.queue <- batch:
// 		log.Printf("Submitted batch %s with %d requests\n", uid, batch.BatchSize)
//
// 		d.onPushComplete(batch)
//
// 	case <-ctx.Done():
// 		log.Println("early, returning from process batch")
// 		return nil, false
//
// 	case <-time.After(DefaultBatchWorkerTimeout):
// 		log.Println("Timing out waiting to enqueue batch", uid)
//
// 		resp.Processing = false
//
// 		if batch.Retries == 2 {
// 			log.Println("Batch queue full, dropping batch:", uid)
//
// 			resp.Retries = -1
// 			return resp, true
// 		}
//
// 		log.Println("re-enqueuing batch", uid, "retries yet", batch.Retries)
//
// 		resp.Retries += 1
// 		// d.batches[uid] = batch
//
// 		return resp, false
// 	}
//
// 	log.Println("===========", len(batch.Jobs))
// 	log.Println("waiting for jobs to complete, for batch", uid, len(batch.Jobs))
// 	now := time.Now()
//
// 	var wg sync.WaitGroup
// 	wg.Add(len(batch.Jobs))
//
// 	for _, job := range batch.Jobs {
// 		go func(j *mediahose.Job) {
// 			defer wg.Done()
//
// 			if r := recover(); r != nil {
// 				log.Printf("💥 panic in job %s: %v", j.ID, r)
// 			}
//
// 			log.Printf("💥 processing in job %s", j.ID)
// 			select {
// 			case <-j.Done:
// 				fmt.Println("===========balls drained=========")
// 			// jobDoneMap[job] = struct{}{}
// 			case <-time.After(DefaultBatchWorkerTimeout):
// 				// TODO:  will need to re-enqueue.
// 				log.Println("timed out trying to process batch, dropping and cancelling")
// 				fmt.Println("timed out")
// 				j.CancelCtx()
//
// 			case <-ctx.Done():
// 				fmt.Println("forcefully exited")
// 			}
// 		}(job)
// 	}
//
// 	log.Println("Waiting for jobs to end")
// 	wg.Wait()
//
// 	// resp.Processing = false
// 	// resp.Retries = 0
//
// 	// restJobs := []*Job{}
// 	//
// 	// // Remove the jobs that are conpleted
// 	// for _, job := range batch.Jobs {
// 	// 	if _, ok := jobDoneMap[job]; !ok {
// 	// 		restJobs = append(restJobs, job)
// 	// 	}
// 	// }
// 	//
// 	// batch.Jobs = restJobs
//
// 	log.Println("all jobs completed for batch", uid, ", in", time.Since(now))
// 	return resp, true
// }
//
// // func OnBatchEnqueueComplete(batch *mediahose.BatchedJob) {
// // 	for _, job := range batch.Jobs {
// // 		err := shared.FlushResponse(job.Resp, func(w http.ResponseWriter) bool {
// // 			u, err := url.Parse(job.OrigPath)
// // 			if err != nil {
// // 				log.Println("failed to parse image path", job.OrigPath)
// // 				return false
// // 			}
// //
// // 			w.Header().Add("Link", shared.BuildLinkHeader(u))
// // 			return true
// // 		})
// // 		if err != nil {
// // 			log.Printf("failed to flush link headers, http2 preload won't work. %v", err)
// // 		}
// // 	}
// //
// // 	log.Println("link headers sent for batch", batch.UID)
// // }
