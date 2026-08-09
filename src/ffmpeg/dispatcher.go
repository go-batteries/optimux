package ffmpeg

import (
	"context"
	"log"
	"sync"
	"time"
)

// VideoDispatcher manages batching of video processing requests
type VideoDispatcher struct {
	mu             sync.Mutex
	batches        map[string]*VideoBatchedJob
	interval       time.Duration
	queue          chan *VideoBatchedJob
	onPushComplete func(batch *VideoBatchedJob)
}

func NewVideoDispatcher(interval time.Duration, queueChan chan *VideoBatchedJob, onPushComplete func(*VideoBatchedJob)) *VideoDispatcher {
	return &VideoDispatcher{
		batches:        make(map[string]*VideoBatchedJob),
		interval:       interval,
		queue:          queueChan,
		onPushComplete: onPushComplete,
	}
}

func (d *VideoDispatcher) RunInBackground(ctx context.Context) chan bool {
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
				log.Println("video dispatcher shutting down, since context cancelled")
				return
			}
		}
	}()

	return done
}

// Add request to a batch (by UID)
func (d *VideoDispatcher) Add(ctx context.Context, uid string, job *VideoJob) {
	d.mu.Lock()
	defer d.mu.Unlock()

	batch, exists := d.batches[uid]
	if !exists {
		batch = &VideoBatchedJob{UID: uid, Jobs: []*VideoJob{job}, BatchSize: 1, CreatedAt: time.Now().UTC()}
		d.batches[uid] = batch
	} else {
		batch.Jobs = append(batch.Jobs, job)
		batch.BatchSize = int32(len(batch.Jobs))
	}
}

// Process all pending batches (called every `interval`)
func (d *VideoDispatcher) ProcessPendingBatches(ctx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, batch := range d.batches {
		if len(batch.Jobs) > 0 {
			log.Printf("🚀 Processing pending video batch %s with %d jobs\n", batch.UID, len(batch.Jobs))

			select {
			case d.queue <- batch:
				log.Printf("📥 Submitted video batch %s with %d requests\n", batch.UID, batch.BatchSize)
				d.onPushComplete(batch)
				delete(d.batches, batch.UID)
			case <-ctx.Done():
				log.Println("early return from process video batch")
				return
			case <-time.After(30 * time.Second):
				log.Println("Timing out waiting to enqueue video batch", batch.UID)
				batch.Retries++
				if batch.Retries >= 3 {
					log.Println("Video batch queue full, dropping batch:", batch.UID)
					delete(d.batches, batch.UID)
				}
			}
		}
	}
}

func NoOpVideoOnComplete(batch *VideoBatchedJob) {
}
