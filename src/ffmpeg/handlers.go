package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/mediahose"
	"github.com/go-batteries/optimux/src/shared"
)

// VideoHandler handles HTTP requests for video processing
type VideoHandler struct {
	VideoQueue    chan *VideoJob
	BatchQueue    chan *VideoBatchedJob
	Dispatcher    *mediahose.Dispatcher
	Encoder       encoders.Encoder
	VideoLoader   VideoLoadStrategy
	DefaultBucket string
	TempDir       string
}

func NewVideoHandler(videoQueue chan *VideoJob, batchQueue chan *VideoBatchedJob,
	dispatcher *mediahose.Dispatcher, encoder encoders.Encoder,
	videoLoader VideoLoadStrategy, defaultBucket, tempDir string) *VideoHandler {
	return &VideoHandler{
		VideoQueue:    videoQueue,
		BatchQueue:    batchQueue,
		Dispatcher:    dispatcher,
		Encoder:       encoder,
		VideoLoader:   videoLoader,
		DefaultBucket: defaultBucket,
		TempDir:       tempDir,
	}
}

// CompressVideoRequest represents a video compression request
type CompressVideoRequest struct {
	VideoURL string `json:"video_url"`
	VideoID  string `json:"video_id"`
	Quality  int    `json:"quality,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	S3Bucket string `json:"s3_bucket,omitempty"`
	S3Key    string `json:"s3_key,omitempty"`
}

// SegmentVideoRequest represents a video segmentation request
type SegmentVideoRequest struct {
	VideoURL string `json:"video_url"`
	VideoID  string `json:"video_id"`
	S3Bucket string `json:"s3_bucket,omitempty"`
	S3Key    string `json:"s3_key,omitempty"`
}

// ExtractFramesRequest represents a frame extraction request
type ExtractFramesRequest struct {
	VideoURL   string           `json:"video_url"`
	VideoID    string           `json:"video_id"`
	StartTime  float64          `json:"start_time,omitempty"`
	Duration   float64          `json:"duration,omitempty"`
	FrameRate  int              `json:"frame_rate,omitempty"`
	PageSize   int              `json:"page_size,omitempty"`
	PageOffset int              `json:"page_offset,omitempty"`
	Boundaries *FrameBoundaries `json:"boundaries,omitempty"`
	S3Bucket   string           `json:"s3_bucket,omitempty"`
	S3Key      string           `json:"s3_key,omitempty"`
}

// HandleVideoCompress handles video compression requests
func (vh *VideoHandler) HandleVideoCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CompressVideoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.VideoURL == "" || req.VideoID == "" {
		http.Error(w, "video_url and video_id are required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Quality == 0 {
		req.Quality = 23 // Good quality for CRF
	}

	bucket := req.S3Bucket
	if bucket == "" {
		bucket = vh.DefaultBucket
	}

	s3Key := req.S3Key
	if s3Key == "" {
		s3Key = fmt.Sprintf("videos/compressed/%s_compressed.mp4", req.VideoID)
	}

	job := &VideoJob{
		ID:              req.VideoID,
		VideoPath:       req.VideoURL,
		Operation:       OperationCompress,
		Quality:         req.Quality,
		Resp:            w,
		Done:            make(shared.DoneCh, 1),
		MailBox:         make(shared.MailBoxCh, 1),
		Ctx:             r.Context(),
		CancelCtx:       func() {},
		Encoder:         vh.Encoder,
		VideoLoader:     vh.VideoLoader,
		OrigPath:        req.VideoURL,
		DefaultS3Bucket: bucket,
		S3Bucket:        &bucket,
		S3Key:           &s3Key,
		Config: &ProcessingConfig{
			Quality: req.Quality,
			Width:   req.Width,
			Height:  req.Height,
		},
		ErrHandler: func(w http.ResponseWriter, msg string, code int) {
			http.Error(w, msg, code)
		},
	}

	select {
	case vh.VideoQueue <- job:
		// Wait for completion
		<-job.Done
	case <-time.After(30 * time.Second):
		http.Error(w, "Request timeout", http.StatusRequestTimeout)
	}
}

// HandleVideoSegment handles video segmentation requests
func (vh *VideoHandler) HandleVideoSegment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SegmentVideoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.VideoURL == "" || req.VideoID == "" {
		http.Error(w, "video_url and video_id are required", http.StatusBadRequest)
		return
	}

	bucket := req.S3Bucket
	if bucket == "" {
		bucket = vh.DefaultBucket
	}

	s3Key := req.S3Key
	if s3Key == "" {
		s3Key = fmt.Sprintf("videos/segments/%s/", req.VideoID)
	}

	job := &VideoJob{
		ID:              req.VideoID,
		VideoPath:       req.VideoURL,
		Operation:       OperationSegment,
		Resp:            w,
		Done:            make(shared.DoneCh, 1),
		MailBox:         make(shared.MailBoxCh, 1),
		Ctx:             r.Context(),
		CancelCtx:       func() {},
		Encoder:         vh.Encoder,
		VideoLoader:     vh.VideoLoader,
		OrigPath:        req.VideoURL,
		DefaultS3Bucket: bucket,
		S3Bucket:        &bucket,
		S3Key:           &s3Key,
		Config:          &ProcessingConfig{},
		ErrHandler: func(w http.ResponseWriter, msg string, code int) {
			http.Error(w, msg, code)
		},
	}

	select {
	case vh.VideoQueue <- job:
		<-job.Done
	case <-time.After(60 * time.Second): // Longer timeout for segmentation
		http.Error(w, "Request timeout", http.StatusRequestTimeout)
	}
}

// HandleExtractFrames handles frame extraction requests with pagination
func (vh *VideoHandler) HandleExtractFrames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	videoURL := query.Get("video_url")
	videoID := query.Get("video_id")

	if videoURL == "" || videoID == "" {
		http.Error(w, "video_url and video_id are required", http.StatusBadRequest)
		return
	}

	// Parse optional parameters
	startTime, _ := strconv.ParseFloat(query.Get("start_time"), 64)
	duration, _ := strconv.ParseFloat(query.Get("duration"), 64)
	frameRate, _ := strconv.Atoi(query.Get("frame_rate"))
	pageSize, _ := strconv.Atoi(query.Get("page_size"))
	pageOffset, _ := strconv.Atoi(query.Get("page_offset"))

	// Set defaults
	if frameRate == 0 {
		frameRate = 13
	}
	if pageSize == 0 {
		pageSize = 50
	}
	if duration == 0 {
		duration = 1.0 // Default to 1 second
	}

	// Parse boundaries if provided
	var boundaries *FrameBoundaries
	if x := query.Get("boundary_x"); x != "" {
		if bx, err := strconv.Atoi(x); err == nil {
			boundaries = &FrameBoundaries{X: bx}
			if y := query.Get("boundary_y"); y != "" {
				if by, err := strconv.Atoi(y); err == nil {
					boundaries.Y = by
				}
			}
			if w := query.Get("boundary_width"); w != "" {
				if bw, err := strconv.Atoi(w); err == nil {
					boundaries.Width = bw
				}
			}
			if h := query.Get("boundary_height"); h != "" {
				if bh, err := strconv.Atoi(h); err == nil {
					boundaries.Height = bh
				}
			}
		}
	}

	bucket := query.Get("s3_bucket")
	if bucket == "" {
		bucket = vh.DefaultBucket
	}

	s3Key := query.Get("s3_key")
	if s3Key == "" {
		s3Key = fmt.Sprintf("videos/frames/%s/", videoID)
	}

	job := &VideoJob{
		ID:              videoID,
		VideoPath:       videoURL,
		Operation:       OperationExtractFrames,
		StartTime:       startTime,
		Duration:        duration,
		FrameRate:       frameRate,
		PageSize:        pageSize,
		PageOffset:      pageOffset,
		Boundaries:      boundaries,
		Resp:            w,
		Done:            make(shared.DoneCh, 1),
		MailBox:         make(shared.MailBoxCh, 1),
		Ctx:             r.Context(),
		CancelCtx:       func() {},
		Encoder:         vh.Encoder,
		VideoLoader:     vh.VideoLoader,
		OrigPath:        videoURL,
		DefaultS3Bucket: bucket,
		S3Bucket:        &bucket,
		S3Key:           &s3Key,
		Config: &ProcessingConfig{
			StartTime:  startTime,
			Duration:   duration,
			FrameRate:  frameRate,
			Boundaries: boundaries,
		},
		ErrHandler: func(w http.ResponseWriter, msg string, code int) {
			http.Error(w, msg, code)
		},
	}

	select {
	case vh.VideoQueue <- job:
		<-job.Done
	case <-time.After(45 * time.Second):
		http.Error(w, "Request timeout", http.StatusRequestTimeout)
	}
}

// HandleBatchVideoProcess handles batch video processing for lambda workers
func (vh *VideoHandler) HandleBatchVideoProcess(ctx context.Context, videoRecords []*VideoLambdaRecord) error {
	batchQueue := make(chan *VideoBatchedJob, len(videoRecords))
	defer close(batchQueue)

	dispatcher := NewVideoDispatcher(100*time.Millisecond, batchQueue, NoOpVideoOnComplete)
	dispatcher.RunInBackground(ctx)

	dones := []shared.MailBoxCh{}

	for _, record := range videoRecords {
		job := &VideoJob{
			ID:              record.VideoID,
			VideoPath:       record.VideoPath,
			Operation:       record.Operation,
			Ctx:             ctx,
			CancelCtx:       func() {},
			Done:            make(shared.DoneCh, 1),
			MailBox:         make(shared.MailBoxCh, 1),
			Encoder:         vh.Encoder,
			VideoLoader:     vh.VideoLoader,
			DefaultS3Bucket: record.Bucket,
			S3Bucket:        &record.Bucket,
			S3Key:           &record.S3Key,
			Config: &ProcessingConfig{
				Quality:    record.Quality,
				StartTime:  record.StartTime,
				Duration:   record.Duration,
				FrameRate:  record.FrameRate,
				Boundaries: record.Boundaries,
			},
			ErrHandler: func(w http.ResponseWriter, msg string, code int) {
				// Lambda context - log errors instead of HTTP response
				fmt.Printf("Video processing error for %s: %s\n", record.VideoID, msg)
			},
		}

		dispatcher.Add(ctx, record.BatchID, job)
		dones = append(dones, job.MailBox)
	}

	// Wait for all jobs to complete
	results := []*VideoProcessingResult{}
	for _, done := range dones {
		select {
		case res := <-done:
			if result, ok := res.(*VideoProcessingResult); ok {
				results = append(results, result)
			}
		case <-time.After(5 * time.Minute):
			// Timeout for lambda processing
		}
	}

	fmt.Printf("Processed %d video jobs, %d completed\n", len(videoRecords), len(results))
	return nil
}

// VideoLambdaRecord represents a video processing record for lambda workers
type VideoLambdaRecord struct {
	VideoID    string
	VideoPath  string
	Operation  ProcessingOperation
	Bucket     string
	S3Key      string
	BatchID    string
	Quality    int
	StartTime  float64
	Duration   float64
	FrameRate  int
	Boundaries *FrameBoundaries
}
