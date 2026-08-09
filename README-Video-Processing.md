# Video Processing Architecture

This document explains how video assets are processed in the Optimux system, from the core layer through the server to the worker pool.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Core Layer](#core-layer)
3. [Server Layer](#server-layer)
4. [Worker Layer](#worker-layer)
5. [Video Processing Pipeline](#video-processing-pipeline)
6. [Configuration](#configuration)
7. [API Examples](#api-examples)

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                      HTTP REQUEST                            │
│  GET /optimux/assets/videos/{user}/{video}.mp4?format=sprites│
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                    SERVER LAYER                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ VideoSpriteHandler                                    │   │
│  │  - Validates format (.sprites, .webvtt)              │   │
│  │  - Creates mediahose.Job                             │   │
│  │  - Submits to Scheduler                              │   │
│  └──────────────────────────────────────────────────────┘   │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                    WORKER LAYER                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ DynamicScaler (Auto-scaled worker pool)              │   │
│  │  - VideoWorker (processes video jobs)                │   │
│  │  - Calls VideoFFmpegProcessor                        │   │
│  └──────────────────────────────────────────────────────┘   │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                     CORE LAYER                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ VideoFFmpegProcessor (orchestrator)                  │   │
│  │  ├─ Sprite generation                                │   │
│  │  ├─ WebVTT generation (ffprobe + template)           │   │
│  │  ├─ Video compression                                │   │
│  │  └─ Frame extraction                                 │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ VideoJobProcessor (executor-based)                   │   │
│  │  ├─ CommandExecutor (ffmpeg, ffprobe)                │   │
│  │  ├─ TemplateExecutor (WebVTT templates)              │   │
│  │  └─ ExecutorFactory (creates executors)              │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

---

## Core Layer

### 1. **VideoJobProcessor** (Execution Engine)

**Location:** `src/ffmpeg/video_processor.go`

**Purpose:** Executes video operations using the executor pattern

**Key Components:**
- **ExecutorFactory**: Creates appropriate executors (CLI, template, AWS)
- **CommandExecutor**: Runs ffmpeg/ffprobe commands
- **TemplateExecutor**: Generates content from templates (WebVTT)

**Flow:**
```go
VideoJobProcessor.Process(ctx, job)
  ├─ Reads action from actions.yaml
  ├─ Creates appropriate executor (exec/template)
  ├─ Executes operation
  └─ Returns result
```

### 2. **VideoFFmpegProcessor** (Orchestrator)

**Location:** `src/mediahose/video_ffmpeg_processor.go`

**Purpose:** High-level orchestration of video operations

**Operations:**
- **Sprites**: Generates sprite sheets (5fps → WebP)
- **WebVTT**: Generates thumbnail VTT files (ffprobe + template)
- **Compression**: Compresses videos
- **Frame Extraction**: Extracts frames at specified FPS

**Example:**
```go
// Sprite generation flow
VideoFFmpegProcessor.generateSprites(ctx, job)
  ├─ Downloads video to cache
  ├─ Creates VideoJobProcessor
  ├─ Calls executor with sprite action
  ├─ Reads generated sprite file
  └─ Returns WebP data
```

### 3. **Executor Types**

#### **CommandExecutor** (CLI Execution)
```go
// Executes: ffmpeg -i input.mp4 -vf fps=5,scale=160:90,tile=10x5 output.webp
executor := NewCommandExecutor(tempDir)
result, err := executor.Execute(ctx, job)
```

#### **TemplateExecutor** (Template Generation)
```go
// Generates WebVTT from template
executor := NewTemplateExecutor(tempDir)
result, err := executor.Execute(ctx, job)
```

---

## Server Layer

### 1. **VideoSpriteHandler**

**Location:** `src/handlers/video_sprite_handler.go`

**Purpose:** HTTP endpoint for sprite/WebVTT generation

**Flow:**
```go
VideoSpriteHandler.ServeHTTP(w, r)
  ├─ Validates format (.sprites, .webvtt)
  ├─ Creates mediahose.Job
  ├─ Sets processor config
  ├─ Submits to Scheduler
  └─ Returns response (WebP or VTT)
```

**Supported Formats:**
- `.sprites` → WebP sprite sheet
- `.webvtt` → WebVTT thumbnail file

**Example Request:**
```bash
GET /optimux/assets/videos/usr_ABC/vid_123.mp4?format=sprites
# Returns: WebP sprite sheet (51KB, 22 frames at 5fps)

GET /optimux/assets/videos/usr_ABC/vid_123.mp4?format=webvtt
# Returns: WebVTT file (1.7KB, 22 cues with sprite coordinates)
```

### 2. **Middleware Chain**

```go
ValidateFormat → SetMediaType → VideoSpriteHandler
  ├─ Validates format parameter
  ├─ Sets MediaType to Video
  └─ Routes to sprite handler
```

---

## Worker Layer

### 1. **DynamicScaler** (Auto-scaled Worker Pool)

**Location:** `src/mediahose/schedulers.go`

**Purpose:** Manages auto-scaling worker pool for video processing

**Configuration:**
```go
VideoWorker:
  - Min Workers: 1
  - Max Workers: 10
  - Scale based on queue depth
```

### 2. **FetchWorker** (Video Worker)

**Purpose:** Processes video jobs from the queue

**Flow:**
```go
FetchWorker.Work(ctx, job)
  ├─ Receives job from scheduler
  ├─ Calls VideoFFmpegProcessor.Process()
  ├─ Handles result
  └─ Sends response
```

---

## Video Processing Pipeline

### **Sprite Generation** (5fps → WebP)

```
1. HTTP Request
   GET /optimux/assets/videos/usr_ABC/vid_123.mp4?format=sprites

2. Server Layer
   VideoSpriteHandler
   ├─ Validates format=sprites
   ├─ Creates Job{Format: "sprites", Quality: 80}
   └─ Submits to Scheduler

3. Worker Layer
   FetchWorker
   ├─ Receives job from queue
   └─ Calls VideoFFmpegProcessor

4. Core Layer
   VideoFFmpegProcessor.generateSprites()
   ├─ Downloads video to /tmp/video_cache/
   ├─ Creates VideoJobProcessor(operation="sprites")
   ├─ Reads config from actions.yaml:
   │   fps: 5
   │   tile_width: 160
   │   tile_height: 90
   │   tile_layout: "10x5"
   ├─ Executes: ffmpeg -i input.mp4 -vf fps=5,scale=160:90,tile=10x5 output.webp
   ├─ Generates: /tmp/shm/video_processing/sprites/vid_123/sprite_sheet.webp
   └─ Returns: 51,664 bytes WebP

5. Response
   Content-Type: image/webp
   Body: <WebP sprite sheet with 22 frames>
```

### **WebVTT Generation** (Composite Operation)

```
1. HTTP Request
   GET /optimux/assets/videos/usr_ABC/vid_123.mp4?format=webvtt

2. Server Layer
   VideoSpriteHandler
   ├─ Validates format=webvtt
   ├─ Creates Job{Format: "webvtt"}
   └─ Submits to Scheduler

3. Worker Layer
   FetchWorker → VideoFFmpegProcessor

4. Core Layer (Composite Operation)
   VideoFFmpegProcessor.generateWebVTTWithExecutor()
   
   Step 1: Run ffprobe (exec executor)
   ├─ Executes: ffprobe -v quiet -print_format json -show_format -show_streams input.mp4
   └─ Extracts: duration=4.40s, fps=24.00, frames=105
   
   Step 2: Calculate sprite frames
   ├─ Reads sprite config from actions.yaml
   ├─ Calculates: 4.40s × 5fps = 22 frames
   ├─ Grid layout: 10x5 (10 frames per row, 5 rows max)
   └─ Frame positions:
       Frame 0: (0,0) → (160,90)
       Frame 1: (160,0) → (320,90)
       ...
       Frame 21: (160,180) → (320,270)
   
   Step 3: Generate WebVTT (template executor)
   ├─ Creates WebVTT with 22 cues
   ├─ Each cue: timestamp + sprite coordinates
   └─ Example:
       00:00:00.000 --> 00:00:00.200
       /sprites/vid_123.webp#xywh=0,0,160,90

5. Response
   Content-Type: text/vtt
   Body: <WebVTT file with 22 cues, 1,736 bytes>
```

### **Video Compression Pipeline** (Multi-Resolution)

**Current:** 5fps sprites → 360p video
**Future:** Support for 720p, 1080p

```
1. Input Video
   - Original: 1280x768, 24fps, 4.4s

2. Sprite Generation (5fps)
   ├─ Extract frames at 5fps
   ├─ Scale to 160x90
   ├─ Tile into 10x5 grid
   └─ Output: sprite_sheet.webp

3. Resolution Downscaling (360p)
   ├─ Target: 640x360
   ├─ Maintains aspect ratio
   ├─ Quality: CRF 23
   └─ Output: video_360p.mp4

4. Future: Multi-Resolution
   ├─ 360p: 640x360 (current)
   ├─ 720p: 1280x720 (future)
   └─ 1080p: 1920x1080 (future)
```

---

## Configuration

### **actions.yaml** (Current Configuration)

```yaml
actions:
  # Sprite generation (5fps → WebP)
  - name: generate_sprites
    defaults:
      fps: 5              # 5 frames per second
      tile_width: 160     # Each tile 160px wide
      tile_height: 90     # Each tile 90px tall
      tile_layout: "10x5" # 10 columns, 5 rows (50 frames max)
      format: jpg
      quality: 3
    executors:
      - type: exec
        command: >
          ffmpeg -i {{.input}} 
          -vf fps={{.fps}},scale={{.tile_width}}:{{.tile_height}},tile={{.tile_layout}} 
          -an -q:v {{.quality}} -y {{.output}}

  # Video compression with configurable resolution
  - name: compress_video
    defaults:
      quality: 23
      preset: medium
      format: mp4
      width: 640      # Default: 360p
      height: 360     # Override via parameters for 720p/1080p
    executors:
      - type: exec
        command: >
          ffmpeg -i {{.input}} -c:v libx264 -crf {{.quality}} 
          -preset {{.preset}} -vf scale={{.width}}:{{.height}} 
          -c:a aac -b:a 128k {{.output}}

  # WebVTT generation (template)
  - name: generate_webvtt
    defaults:
      sprite_url: "/sprites/{{.video_id}}.webp"
      tile_width: 160
      tile_height: 90
      frames_per_row: 10  # Matches tile_layout
    executors:
      - type: template
        template: |
          WEBVTT
          
          {{range .frames}}
          {{.start_time}} --> {{.end_time}}
          {{$.sprite_url}}#xywh={{.x}},{{.y}},{{.width}},{{.height}}
          {{end}}

  # ffprobe (video metadata)
  - name: probe_video
    defaults:
      format: json
    executors:
      - type: exec
        command: >
          ffprobe -v quiet -print_format {{.format}} 
          -show_format -show_streams {{.input}}
```

### **Resolution Override Examples**

```yaml
# Single action, multiple resolutions via parameter override
- name: compress_video
  defaults:
    width: 640   # 360p default
    height: 360
  
# Usage:
# 360p: Use defaults (640x360)
# 720p: Override with width=1280, height=720
# 1080p: Override with width=1920, height=1080
```

---

## API Examples

### **1. Generate Sprite Sheet**

```bash
# Request
GET /optimux/assets/videos/usr_PnsjzXA7oX/vid_08M5HTMovu.mp4?format=sprites

# Response
Content-Type: image/webp
Content-Length: 51664

<WebP sprite sheet with 22 frames>
```

**Processing:**
- Video: 4.4s duration, 24fps
- Sprite: 5fps sampling = 22 frames
- Grid: 10x5 layout
- Output: 51,664 bytes WebP

### **2. Generate WebVTT**

```bash
# Request
GET /optimux/assets/videos/usr_PnsjzXA7oX/vid_08M5HTMovu.mp4?format=webvtt

# Response
Content-Type: text/vtt
Content-Length: 1736

WEBVTT

00:00:00.000 --> 00:00:00.200
/sprites/vid_08M5HTMovu.webp#xywh=0,0,160,90

00:00:00.200 --> 00:00:00.400
/sprites/vid_08M5HTMovu.webp#xywh=160,0,160,90

...
```

**Processing:**
- Runs ffprobe to get video properties
- Calculates 22 sprite frames at 5fps
- Generates WebVTT with accurate timestamps
- Each cue: 0.2s interval (1/5fps)

### **3. Video Transcoding with Presets**

**Serves transcoded videos from EFS cache (fast!) or S3**

```bash
# Request: Original video
GET /optimux/assets/videos/usr_ABC/vid_123.mp4

# Request: 360p transcoded version
GET /optimux/assets/videos/usr_ABC/vid_123.mp4?preset=360p

# Request: 720p transcoded version
GET /optimux/assets/videos/usr_ABC/vid_123.mp4?preset=720p

# Response: Redirect to S3 or serve from EFS
HTTP/1.1 302 Found
Location: https://s3.../stg/videos/usr_ABC/transcoded/vid_123_360p.mp4
Cache-Control: public, max-age=31536000
```

**How It Works:**
```
1. HTTP Request with ?preset=360p
   ↓
2. Check EFS cache first (fast!)
   Path: /tmp/shm/image_cache/videos/transcoded/vid_123_compressed.mp4
   ↓
3. If in EFS: Serve directly (no S3 call needed)
   ↓
4. If not in EFS: Check S3
   Path: stg/videos/usr_ABC/transcoded/vid_123_360p.mp4
   ↓
5. If in S3: Redirect to S3 URL
   If not: Return 404 (transcode job needed)
```

**Storage Locations:**
- **EFS Cache**: `/tmp/shm/image_cache/videos/transcoded/` (persistent, shared across containers)
- **S3 Backup**: `s3://{bucket}/stg/videos/{user}/transcoded/` (permanent storage)

**Directory Structure:**
```
/tmp/shm/image_cache/          ← EFS mount (persistent, shared)
└── videos/
    ├── sprites/               ← Sprite sheets (50KB each)
    │   └── vid_123/
    │       └── sprite_sheet.webp
    └── transcoded/            ← Transcoded videos (several MB)
        ├── vid_123_compressed.mp4
        └── vid_456_compressed.mp4

/tmp/shm/edge_cache/           ← Nginx proxy cache (tmpfs, RAM)
└── (Nginx caches HTTP responses here for 60m)
```

**Why EFS for both:**
- ✅ **Persistent** - Survives container restarts
- ✅ **Shared** - All containers can access
- ✅ **Protected** - Sidecar cleaner won't delete
- ✅ **Nginx still caches** - HTTP responses cached in edge_cache

**Supported Presets:**
- `360p` → 640x360
- `480p` → 854x480  
- `720p` → 1280x720
- `1080p` → 1920x1080

**Note:** Transcoded videos are saved to EFS for fast access, similar to how sprite sheets are cached.

---

## Key Features

### **1. Config-Driven Processing**
- All operations defined in `actions.yaml`
- Easy to add new resolutions/formats
- Safe type conversion (handles int/float64)

### **2. Dynamic Frame Calculation**
- Uses actual video properties (duration, fps)
- Calculates exact frame count and positions
- Synchronizes sprite sheet with WebVTT

### **3. Executor Pattern**
- **CommandExecutor**: CLI operations (ffmpeg, ffprobe)
- **TemplateExecutor**: Template generation (WebVTT)
- **Future**: AWS MediaConvert, Lambda executors

### **4. Worker Pool Integration**
- Auto-scaling based on queue depth
- Processes jobs asynchronously
- Returns results via HTTP response

---

## Next Steps

### **Immediate:**
1. ✅ Sprite generation (5fps → WebP)
2. ✅ WebVTT generation (ffprobe + template)
3. ✅ Config-driven calculations

### **Future Enhancements:**
1. **Multi-Resolution Support** ✅
   - Single `compress_video` action with configurable resolution
   - Support for 360p, 720p, 1080p via parameters
   - Dynamic resolution based on request

2. **LCEVC Enhancement Codec** (Optional)
   - MPEG-5 Part 2 LCEVC for better compression
   - 30-40% bitrate savings at same quality
   - Same resolution, enhanced encoding
   - Note: Requires LCEVC decoder support (limited browser support)
   - Use case: Smaller files without quality loss

3. **Frame Rate Downsampling**
   - 5fps → 1fps for longer videos
   - Adaptive FPS based on duration

4. **Advanced Features**
   - Scene detection
   - Thumbnail generation
   - Video segmentation (1s intervals)

---

## Troubleshooting

### **Common Issues**

**1. Sprite generation fails**
- Check ffmpeg is installed: `ffmpeg -version`
- Verify video cache: `/tmp/video_cache/`
- Check logs for ffmpeg errors

**2. WebVTT has wrong timestamps**
- Verify ffprobe output
- Check sprite config (fps, tile_layout)
- Ensure frame count matches sprite sheet

**3. Worker pool not processing**
- Check queue depth: `DynamicScaler` logs
- Verify worker count
- Check for errors in worker logs

### **Debug Mode**

```bash
# Enable verbose logging
export VIDEO_EXECUTOR_DEBUG=true

# Check worker status
curl http://localhost:8811/debug/workers
```

---

## Summary

The video processing system follows a **three-layer architecture**:

1. **Core Layer**: Executors and processors (ffmpeg, ffprobe, templates)
2. **Server Layer**: HTTP handlers and middleware
3. **Worker Layer**: Auto-scaled worker pool

**Key Flow:**
```
HTTP Request → Handler → Scheduler → Worker → Processor → Executor → FFmpeg
```

**Current Capabilities:**
- ✅ Sprite generation (5fps, 10x5 grid)
- ✅ WebVTT generation (dynamic, config-driven)
- ✅ Video compression (360p)
- ✅ Config-driven operations

**Future:**
- 🔄 Multi-resolution (720p, 1080p)
- 🔄 Adaptive frame rates
- 🔄 Advanced video operations
