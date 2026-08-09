# Optimux: Dynamic Media Processing Architecture

**Version:** 1.0  
**Date:** 2024  
**Author:** Technical Architecture Team

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [System Overview](#system-overview)
3. [Architecture Components](#architecture-components)
4. [Dynamic Worker Scaling](#dynamic-worker-scaling)
5. [Go Channel Structures](#go-channel-structures)
6. [Caching Strategy](#caching-strategy)
7. [HTTP/2 Implementation](#http2-implementation)
8. [S3 Integration & Backup](#s3-integration--backup)
9. [Digital Asset Management (DAM)](#digital-asset-management-dam)
10. [Infrastructure Scaling](#infrastructure-scaling)
11. [Use Cases & Sizing Requirements](#use-cases--sizing-requirements)
12. [Performance Characteristics](#performance-characteristics)

---

## Executive Summary

Optimux is a high-performance, dynamically-scalable media processing system designed to handle on-demand image and video transformations for Digital Asset Management (DAM) platforms. The system addresses the critical challenge of serving multiple image/video sizes and formats to various listing websites (e-commerce, real estate, social media) without pre-generating all possible combinations.

### Key Innovations

- **Dynamic Worker Scaling**: Automatic worker pool adjustment based on queue depth
- **Multi-tier Caching**: Edge (Nginx) → tmpfs → EFS → S3 hierarchy
- **HTTP/2 Optimization**: Parallel resource hints via Link headers
- **S3 Header-based Optimization**: Metadata-driven cache validation
- **Dual-mode Processing**: Real-time server processing + batch worker processing

### Problem Solved

Traditional DAM systems require pre-generating dozens of image sizes (thumbnails, previews, full-size) for each asset, consuming massive storage and processing time upfront. Optimux generates sizes **on-demand** and caches them intelligently, reducing:
- Initial upload processing time by 80%+
- Storage costs by 60%+
- Time-to-first-publish from minutes to seconds

---

## System Overview

### Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                         CLIENT LAYER                             │
│  (E-commerce sites, Real Estate portals, Social Media platforms) │
└────────────────┬────────────────────────────────────────────────┘
                 │ HTTPS/HTTP2
                 ▼
┌─────────────────────────────────────────────────────────────────┐
│                      NGINX EDGE CACHE                            │
│  • Proxy cache: /tmp/shm/edge_cache (960MB tmpfs)               │
│  • Cache duration: 60 minutes                                    │
│  • HTTP/2 enabled with SSL                                       │
└────────────────┬────────────────────────────────────────────────┘
                 │ Cache MISS
                 ▼
┌─────────────────────────────────────────────────────────────────┐
│                    OPTIMUX SERVER (Go)                           │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  HTTP/2 Server (TLS, MaxConcurrentStreams: 250)         │   │
│  └──────────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Image Queue (10,000 cap)  │  Video Queue (2,500 cap)   │   │
│  └──────────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  DynamicScaler[Image]      │  DynamicScaler[Video]       │   │
│  │  • Min: 10 workers         │  • Min: 2 workers           │   │
│  │  • Max: 200 workers        │  • Max: 50 workers          │   │
│  │  • Scale up: >75% queue    │  • Scale up: >60% queue     │   │
│  │  • Scale down: <25% queue  │  • Scale down: <20% queue   │   │
│  └──────────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Worker Pool (FetchWorker instances)                     │   │
│  │  • ImageProcessor (libvips)                              │   │
│  │  • VideoFFmpegProcessor (FFmpeg)                         │   │
│  └──────────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Caching Layer                                           │   │
│  │  • tmpfs: /tmp/shm/image_cache (RAM-based)              │   │
│  │  • Disk: /tmp/video_cache (persistent)                  │   │
│  └──────────────────────────────────────────────────────────┘   │
└────────────────┬────────────────────────────────────────────────┘
                 │
                 ├─────────────┐
                 │             │
                 ▼             ▼
┌─────────────────────┐  ┌──────────────────────────────────────┐
│    AWS S3           │  │    AWS SQS + Lambda Worker           │
│  • Original assets  │  │  • Batch processing                  │
│  • Processed cache  │  │  • Pre-warming cache                 │
│  • Metadata headers │  │  • Async transformations             │
└─────────────────────┘  └──────────────────────────────────────┘
```

### Component Responsibilities

#### 1. **Server Layer** (`cmd/server/main.go`)
- HTTP/2 request handling with TLS
- Dynamic worker pool management
- Real-time image/video processing
- Cache coordination
- Link header generation for HTTP/2 push

#### 2. **Worker Layer** (`cmd/worker/main.go`)
- AWS Lambda-based batch processing
- SQS event consumption
- Pre-computation of common sizes
- S3 metadata updates

#### 3. **Core Layer** (`src/mediahose/`)
- Generic `DynamicScaler[T]` for worker management
- `FetchWorker` and `BatchWorker` implementations
- Job queuing and dispatching
- Event-driven batch processing

---

## Architecture Components

### Entry Points

#### Server Entry Point
**File:** `cmd/server/main.go`

**Responsibilities:**
1. Initialize AWS S3 client for storage operations
2. Create separate job queues for images and videos
3. Bootstrap DynamicScaler instances with different configurations
4. Register HTTP handlers with middleware chain
5. Configure HTTP/2 server with TLS certificates
6. Start graceful shutdown handlers

**Key Configuration:**
```go
imageQueue := make(chan *mediahose.Job, cfg.QSize)        // 10,000 capacity
videoQueue := make(chan *mediahose.Job, cfg.QSize/4)      // 2,500 capacity

imageScaler := &mediahose.DynamicScaler[*mediahose.Job]{
    MinWorkers:         10,
    MaxWorkers:         200,
    ScaleUpThreshold:   int(float64(cfg.QSize) * 0.75),   // 7,500 jobs
    ScaleDownThreshold: int(float64(cfg.QSize) * 0.25),   // 2,500 jobs
}

videoScaler := &mediahose.DynamicScaler[*mediahose.Job]{
    MinWorkers:         2,
    MaxWorkers:         50,
    ScaleUpThreshold:   int(float64(cfg.QSize/4) * 0.60), // 1,500 jobs
    ScaleDownThreshold: int(float64(cfg.QSize/4) * 0.20), // 500 jobs
}
```

#### Worker Entry Point
**File:** `cmd/worker/main.go`

**Responsibilities:**
1. Run as AWS Lambda function
2. Process SQS events containing media transformation jobs
3. Connect to PostgreSQL for metadata storage
4. Execute batch transformations
5. Upload results to S3 with metadata headers

**Execution Model:**
- Event-driven via SQS
- Batch processing (multiple jobs per invocation)
- Stateless execution
- Auto-scaling via Lambda concurrency

---

## Dynamic Worker Scaling

### The DynamicScaler Pattern

The `DynamicScaler[T]` is a **generic Go struct** that manages worker pools dynamically based on queue pressure. This is the core innovation enabling cost-efficient scaling.

**File:** `src/mediahose/schedulers.go`

### Architecture

```go
type DynamicScaler[T any] struct {
    Queue              chan T           // Job queue channel
    MinWorkers         int              // Minimum worker count
    MaxWorkers         int              // Maximum worker count
    ActiveWorkers      int              // Current worker count
    ScaleUpThreshold   int              // Queue length to trigger scale-up
    ScaleDownThreshold int              // Queue length to trigger scale-down
    ScaleSigChan       chan struct{}    // Manual scaling signal channel
    workers            []workerContext  // Worker contexts for cancellation
    workerFactory      Worker[T]        // Factory to create workers
    mu                 sync.RWMutex     // Protects worker slice
}

type workerContext struct {
    cancel context.CancelFunc  // Cancellation function for graceful shutdown
}
```

### Scaling Algorithm

The scaler runs a background goroutine that periodically checks queue depth:

```go
func (ds *DynamicScaler[T]) scale(ctx context.Context) {
    queueLen := len(ds.Queue)
    
    // Scale UP: Queue is filling, add workers
    if queueLen > ds.ScaleUpThreshold && ds.ActiveWorkers < ds.MaxWorkers {
        ds.addWorker(ctx)
        log.Printf("⬆️ Scaled UP: %d workers (queue: %d/%d)", 
            ds.ActiveWorkers, queueLen, cap(ds.Queue))
        return
    }
    
    // Scale DOWN: Queue is draining, remove workers
    if queueLen < ds.ScaleDownThreshold && ds.ActiveWorkers > ds.MinWorkers {
        ds.removeWorker()
        log.Printf("⬇️ Scaled DOWN: %d workers (queue: %d/%d)", 
            ds.ActiveWorkers, queueLen, cap(ds.Queue))
        return
    }
}
```

### Worker Lifecycle Management

#### Adding Workers

```go
func (ds *DynamicScaler[T]) addWorker(ctx context.Context) {
    workerCtx, cancel := context.WithCancel(ctx)
    
    // Create worker context for later cancellation
    ds.workers = append(ds.workers, workerContext{cancel: cancel})
    ds.ActiveWorkers++
    
    // Start worker goroutine
    go ds.workerFactory.Work(workerCtx, ds.Queue)
}
```

#### Removing Workers

```go
func (ds *DynamicScaler[T]) removeWorker() {
    if len(ds.workers) == 0 {
        return
    }
    
    // Cancel the last worker's context
    lastWorker := ds.workers[len(ds.workers)-1]
    lastWorker.cancel()
    
    // Remove from slice
    ds.workers = ds.workers[:len(ds.workers)-1]
    ds.ActiveWorkers--
}
```

### Graceful Shutdown

```go
func (ds *DynamicScaler[T]) Shutdown() {
    ds.mu.Lock()
    defer ds.mu.Unlock()
    
    // Cancel all worker contexts
    for _, worker := range ds.workers {
        worker.cancel()
    }
    
    // Close the job queue
    close(ds.Queue)
    
    log.Printf("✅ Shutdown complete: all %d workers stopped", len(ds.workers))
}
```

### Scaling Triggers

#### Automatic Scaling
- **Periodic Check**: Every 500ms, the scaler evaluates queue depth
- **Threshold-based**: Compares queue length against configured thresholds

#### Manual Scaling
- **Signal Channel**: Handlers can send signals to `ScaleSigChan`
- **Use Case**: Immediate scale-up when queue usage exceeds 75%

```go
// In S3ProxyImageHandler
queueUsage := float64(len(s.JobQ)) / float64(cap(s.JobQ))
if queueUsage > 0.75 && s.Scaler.ActiveWorkers < s.Scaler.MaxWorkers {
    log.Printf("⚠️ Queue at %.2f%% capacity, scaling up!", queueUsage*100)
    s.Scaler.ScaleSigChan <- struct{}{}
}
```

### Worker Types

#### FetchWorker (Real-time Processing)

```go
type FetchWorker struct {
    S3Client *s3.Client
}

func (fw *FetchWorker) Work(ctx context.Context, jobQueue <-chan *Job) {
    for {
        select {
        case <-ctx.Done():
            return  // Graceful shutdown
            
        case job := <-jobQueue:
            var processor MediaProcessor
            
            if job.MediaType == MediaTypeImage {
                processor = &ImageProcessor{
                    S3Client: fw.S3Client,
                }
            } else if job.MediaType == MediaTypeVideo {
                processor = &VideoFFmpegProcessor{
                    S3Client: fw.S3Client,
                    TempDir:  "/tmp/shm/video_processing",
                }
            }
            
            // Process the job
            bytes, err := processor.Process(ctx, job)
            if err != nil {
                job.ErrHandler(job.Resp, err)
                close(job.Done)
                continue
            }
            
            // Encode and send response
            job.Encoder(job.Resp, bytes, job)
            close(job.Done)  // Signal completion
        }
    }
}
```

#### BatchWorker (Async Processing)

```go
type BatchWorker struct {
    S3Client *s3.Client
    DB       *sql.DB
}

func (bw *BatchWorker) Work(ctx context.Context, batchQueue <-chan *BatchedJob) {
    for {
        select {
        case <-ctx.Done():
            return
            
        case batch := <-batchQueue:
            // Process all jobs in batch concurrently
            var wg sync.WaitGroup
            semaphore := make(chan struct{}, 10)  // Limit concurrency
            
            for _, job := range batch.Jobs {
                wg.Add(1)
                semaphore <- struct{}{}
                
                go func(j *Job) {
                    defer wg.Done()
                    defer func() { <-semaphore }()
                    
                    // Process and upload to S3
                    processor := &ImageProcessor{S3Client: bw.S3Client}
                    bytes, err := processor.Process(ctx, j)
                    if err != nil {
                        log.Printf("Batch job failed: %v", err)
                        return
                    }
                    
                    // Upload to S3 with metadata
                    s3Key := fmt.Sprintf("%s/%s", j.ID, j.Format)
                    bw.uploadToS3(ctx, s3Key, bytes, j)
                    
                    // Update database metadata
                    bw.updateMetadata(ctx, j)
                }(job)
            }
            
            wg.Wait()
            close(batch.Done)
        }
    }
}
```

### Scaling Characteristics

| Metric | Image Workers | Video Workers |
|--------|--------------|---------------|
| **Min Workers** | 10 | 2 |
| **Max Workers** | 200 | 50 |
| **Queue Capacity** | 10,000 | 2,500 |
| **Scale Up Threshold** | 7,500 jobs (75%) | 1,500 jobs (60%) |
| **Scale Down Threshold** | 2,500 jobs (25%) | 500 jobs (20%) |
| **Check Interval** | 500ms | 500ms |
| **Avg Processing Time** | 50-200ms | 2-10s |

### Why Different Configurations?

**Images:**
- Higher throughput (smaller files, faster processing)
- More concurrent requests expected
- Lower memory per worker (~50MB)
- Can scale aggressively

**Videos:**
- Lower throughput (larger files, FFmpeg processing)
- Fewer concurrent requests
- Higher memory per worker (~500MB)
- Conservative scaling to avoid OOM

---

## Go Channel Structures

### Channel Types and Usage

Optimux uses Go channels extensively for job queuing, worker coordination, and event signaling.

### 1. Job Queue Channels

**Purpose:** Distribute work to worker pools

```go
// Buffered channels for job queuing
imageQueue := make(chan *mediahose.Job, 10000)
videoQueue := make(chan *mediahose.Job, 2500)
batchQueue := make(chan *mediahose.BatchedJob, 100)
```

**Characteristics:**
- **Buffered**: Prevents blocking when workers are busy
- **Capacity**: Sized based on expected load
- **Type-safe**: Strongly typed to `*Job` or `*BatchedJob`

**Flow:**
```
HTTP Handler → Enqueue Job → Job Queue → Worker Pool → Process → Response
```

### 2. Done Channels

**Purpose:** Signal job completion

```go
type Job struct {
    Done chan struct{}  // Closed when job completes
    // ... other fields
}

// In handler
job := &Job{
    Done: make(chan struct{}),
    // ...
}

jobQueue <- job
<-job.Done  // Block until worker closes the channel
```

**Pattern:**
- **Unbuffered**: Synchronization point
- **Close-only**: Never send data, only close
- **Idiomatic**: Standard Go completion signal

### 3. Scaling Signal Channels

**Purpose:** Trigger immediate worker scaling

```go
type DynamicScaler[T] struct {
    ScaleSigChan chan struct{}  // Manual scaling trigger
}

// Trigger scaling
scaler.ScaleSigChan <- struct{}{}
```

**Usage:**
- Handler detects high queue usage
- Sends signal for immediate scale-up
- Scaler responds within milliseconds

### 4. Context Cancellation

**Purpose:** Graceful worker shutdown

```go
// Create cancellable context for each worker
workerCtx, cancel := context.WithCancel(parentCtx)

// Store cancel function
workers = append(workers, workerContext{cancel: cancel})

// Worker respects context
func (fw *FetchWorker) Work(ctx context.Context, jobQueue <-chan *Job) {
    for {
        select {
        case <-ctx.Done():
            return  // Exit gracefully
        case job := <-jobQueue:
            // Process job
        }
    }
}
```

### 5. Mailbox Channels

**Purpose:** Send additional data to jobs after enqueue

```go
type Job struct {
    MailBox chan []byte  // Buffered channel for async communication
}

job := &Job{
    MailBox: make(chan []byte, 1),
}
```

**Use Case:**
- Send S3 pre-signed URLs
- Provide cache hints
- Update job parameters mid-flight

### Channel Communication Patterns

#### Pattern 1: Fan-Out (Job Distribution)

```
                    ┌─→ Worker 1
                    │
Job Queue ──────────┼─→ Worker 2
(1 channel)         │
                    ├─→ Worker 3
                    │
                    └─→ Worker N
```

**Implementation:**
- Multiple workers read from same channel
- Go runtime handles distribution
- First available worker gets the job

#### Pattern 2: Fan-In (Batch Collection)

```
Job 1 ──┐
Job 2 ──┤
Job 3 ──┼─→ Dispatcher ─→ BatchedJob ─→ Batch Queue
Job 4 ──┤
Job 5 ──┘
```

**Implementation:**
```go
type Dispatcher struct {
    batches map[string]*BatchedJob
}

func (d *Dispatcher) Enqueue(job *Job) {
    batch, exists := d.batches[job.UID]
    if !exists {
        batch = &BatchedJob{
            UID:  job.UID,
            Jobs: []*Job{},
            Done: make(chan struct{}),
        }
        d.batches[job.UID] = batch
    }
    batch.Jobs = append(batch.Jobs, job)
}
```

#### Pattern 3: Event Emission

```go
type EventEmitter struct {
    events map[string][]EventCallback
}

func (e *EventEmitter) Emit(ctx context.Context, event string, data interface{}) <-chan struct{} {
    done := make(chan struct{})
    
    go func() {
        defer close(done)
        for _, callback := range e.events[event] {
            callback(ctx, data)
        }
    }()
    
    return done
}
```

### Channel Best Practices in Optimux

1. **Always use buffered channels for queues** - Prevents blocking producers
2. **Close channels to signal completion** - Idiomatic Go pattern
3. **Use context for cancellation** - Enables graceful shutdown
4. **Select with context.Done()** - Allows worker interruption
5. **Avoid closing channels from receivers** - Only senders close

