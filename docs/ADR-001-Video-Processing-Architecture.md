# ADR-001: Video Processing Architecture Strategy

**Status:** Accepted  
**Date:** 2025-01-12  
**Deciders:** Engineering Team  

## Context

We need to implement a scalable video processing system that handles:
- Automatic video compression on S3 upload
- Frame extraction for scrubbing/preview functionality
- Support for mp4, webm, avi formats
- Cost-effective storage at scale (10K-1M videos/month)
- Sub-second latency for video scrubbing

## Decision

**Hybrid Storage Strategy: Baseline Spritesheets + On-Demand High-Fidelity Frames**

### Architecture Components

1. **S3 Event-Driven Processing**
   - SNS triggers on video upload
   - Format detection (mp4/webm/avi)
   - Automatic compression pipeline

2. **Baseline Processing (Always Generated)**
   - Compressed video (H.264, quality 23-28)
   - Low-FPS spritesheets (6fps, 30 frames per sprite)
   - Metadata manifest with frame mapping

3. **On-Demand Processing (Zoom/Precision)**
   - High-FPS frame extraction (24-30fps)
   - 1-second burst windows
   - EFS cache with TTL (24h)

4. **Storage Layout**
   ```
   ${bucket}/videos/{video_id}/
   ├── compressed.mp4
   ├── sprites/
   │   ├── sprite_00.webp (frames 0-29)
   │   ├── sprite_01.webp (frames 30-59)
   │   └── manifest.json
   └── frames/ (on-demand cache)
       ├── frames_00001.jpg
       └── frames_00002.jpg
   ```

## Cost Analysis (Monthly)

### 10K Videos/Month (2min avg, 30fps)
- **Storage**: ~30GB spritesheets + 200GB compressed videos = 230GB
- **S3 Costs**: $5.29/month storage + $0.50 PUT requests
- **ECS Compute**: ~50 vCPU hours = $25/month
- **Total**: ~$31/month

### 100K Videos/Month
- **Storage**: ~2.3TB
- **S3 Costs**: $52.90/month storage + $5 PUT requests  
- **ECS Compute**: ~500 vCPU hours = $250/month
- **Total**: ~$308/month

### 1M Videos/Month
- **Storage**: ~23TB
- **S3 Costs**: $529/month storage + $50 PUT requests
- **ECS Compute**: ~5,000 vCPU hours = $2,500/month
- **Total**: ~$3,079/month

## Implementation Details

### 1. SNS/S3 Event Processing

```go
// S3 event handler
func HandleS3VideoUpload(event S3Event) {
    if isVideoFormat(event.Object.Key) {
        job := &VideoProcessingJob{
            VideoID: generateID(),
            S3Key:   event.Object.Key,
            Actions: []string{"compress", "generate_sprites"},
        }
        dispatcher.Add(job)
    }
}
```

### 2. FFmpeg DSL Configuration

```yaml
actions:
  - name: compress_video
    defaults:
      quality: 23
      preset: medium
      format: mp4
    command: |
      ffmpeg -i {{.input}} -c:v libx264 -crf {{.quality}} 
      -preset {{.preset}} -c:a aac -b:a 128k {{.output}}
    
  - name: generate_sprites  
    defaults:
      fps: 6
      frames_per_sprite: 30
      format: webp
    command: |
      ffmpeg -i {{.input}} -vf fps={{.fps}} -f image2 
      /tmp/frames_%05d.jpg && 
      montage /tmp/frames_*.jpg -tile 6x5 -geometry +0+0 {{.output}}
```

### 3. Worker Dockerfile

```dockerfile
FROM ubuntu:22.04

# Install FFmpeg with all codecs
RUN apt-get update && apt-get install -y \
    ffmpeg \
    libx264-dev \
    libx265-dev \
    libvpx-dev \
    libaom-dev \
    libsvtav1-dev \
    libwebp-dev \
    imagemagick \
    && rm -rf /var/lib/apt/lists/*

# Configure ImageMagick for montage operations
RUN sed -i 's/rights="none" pattern="PDF"/rights="read|write" pattern="PDF"/' /etc/ImageMagick-6/policy.xml

COPY optimux-worker /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/optimux-worker"]
```

### 4. ECS Scaling Configuration

```yaml
# CloudWatch metrics for autoscaling
metrics:
  - name: SQSQueueDepth
    threshold: 10
    scale_up_cooldown: 300s
    scale_down_cooldown: 600s
  
  - name: CPUUtilization  
    threshold: 70%
    
  - name: MemoryUtilization
    threshold: 80%

# ECS service configuration
min_capacity: 2
max_capacity: 50
target_cpu: 70
target_memory: 80
```

## Alternatives Considered

### A. Store All Frames (Rejected)
- **Cost**: 100x higher storage ($3,000/month for 100K videos)
- **Complexity**: Millions of S3 objects, high PUT/GET costs
- **Benefit**: Simplest frontend implementation

### B. Pure On-Demand (Rejected)  
- **Latency**: 200-500ms cold start for frame extraction
- **CPU Cost**: 10x higher compute costs during peak scrubbing
- **Benefit**: Minimal storage costs

### C. Pre-computed High-FPS (Rejected)
- **Storage**: 10x higher than spritesheets
- **Waste**: Most users never zoom beyond 6fps precision
- **Benefit**: Consistent low latency

## Consequences

### Positive
- **Cost Efficient**: 90% storage reduction vs all-frames approach
- **Scalable**: Linear cost scaling with video volume
- **Performance**: <100ms sprite loading, <500ms on-demand frames
- **Flexible**: Supports both casual viewing and precision editing

### Negative  
- **Complexity**: Dual storage system requires careful cache management
- **Latency**: On-demand frames have variable latency (200-500ms)
- **Cache Management**: EFS cleanup and TTL policies needed

## Monitoring & Metrics

```yaml
alerts:
  - name: VideoProcessingBacklog
    condition: SQS depth > 100 for 5min
    
  - name: FrameExtractionLatency  
    condition: P95 > 1000ms for 2min
    
  - name: StorageCostAnomaly
    condition: Daily storage growth > 2x expected
    
  - name: ECSTaskFailures
    condition: Task failure rate > 5% for 10min
```

## Implementation Priority

1. **Phase 1**: Basic compression + sprite generation
2. **Phase 2**: On-demand frame extraction with EFS cache  
3. **Phase 3**: Advanced scaling metrics and cost optimization
4. **Phase 4**: Multi-region deployment and CDN integration

---

**Risk Level**: Medium  
**Estimated Implementation**: 4-6 weeks  
**Review Date**: 2025-04-12
