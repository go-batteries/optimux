package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/roverxio/optimux/src/encoders"
	"github.com/roverxio/optimux/src/mediahose"
	"github.com/roverxio/optimux/src/shared"
)

// ImageHandlerAsync processes images and queues them for workers
type ImageHandlerAsync struct {
	JobQ   chan *mediahose.Job
	Scaler *mediahose.DynamicScaler[*mediahose.Job]
}

func (s *ImageHandlerAsync) Handle(quality int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer shared.Bench("image resize handler")()

		query := r.URL.Query()
		imagePath := query.Get("image_url")
		if imagePath == "" {
			http.Error(w, "Missing image URL", http.StatusBadRequest)
			return
		}

		id := shared.CalculateHash(imagePath)

		derievedFormat := filepath.Ext(imagePath)
		format := query.Get("format")
		if format == "" {
			format = derievedFormat
		} else {
			format = fmt.Sprintf(".%s", format)
		}

		// if format != ".jpeg" && format != ".webp" {
		// 	http.Error(w, "Invalid format", http.StatusBadRequest)
		// 	return
		// }

		var sizes [][2]int

		if sizesParam, ok := query["sizes"]; ok {
			sizes = shared.SanitizeSizes(sizesParam)
		}

		if len(sizes) == 0 {
			http.Error(w, "Invalid sizes, use 'sizes=800x600,400x0'", http.StatusBadRequest)
			return
		}

		qualityParams := query.Get("quality")
		if qualityParams != "" {
			q, err := strconv.Atoi(qualityParams)
			if err == nil {
				quality = shared.ClampValue(0, 100, q)
			}
		}

		encoderParams := query.Get("encoder")
		if encoderParams == "" {
			encoderParams = "stream"
		}

		encoder := encoders.StreamEncoder
		if encoderParams == "json" {
			encoder = encoders.Base64Encoder
		} else if encoderParams == "progress" {
			encoder = encoders.ProgressiveStreamEncoder
		}

		var strategy mediahose.LoadImageStrategy

		thumbParam := query.Get("thumb")
		if thumbParam == "true" {
			strategy = mediahose.NewThumbMediaLoader()
		} else {
			strategy = mediahose.NewImageMediaLoader()
		}

		mediaType, ok := r.Context().Value("media_type").(mediahose.MediaType)
		if !ok {
			http.Error(w, "media type not found", http.StatusInternalServerError)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		job := &mediahose.Job{
			ID:          id,
			ImagePath:   imagePath,
			Format:      format,
			Sizes:       sizes,
			Quality:     quality,
			Resp:        w,
			Ctx:         ctx,
			Done:        make(shared.DoneCh),
			Encoder:     encoder,
			ErrHandler:  shared.ResponseWriter,
			MediaType:   mediaType,
			ImageLoader: strategy,
		}

		select {
		case s.JobQ <- job:
			queueUsage := float64(len(s.JobQ)) / float64(cap(s.JobQ))
			if queueUsage > 0.75 && s.Scaler.ActiveWorkers < s.Scaler.MaxWorkers {
				log.Printf("⚠️  Queue is at %.2f%% capacity, scaling up workers!", queueUsage*100)
				s.Scaler.ScaleSigChan <- struct{}{}
			}

		case <-time.After(shared.DefaultWaitTillEnQTime):
			http.Error(w, "Server too busy", http.StatusServiceUnavailable)
			return
		}

		if len(sizes) > 1 {
			u := r.Clone(r.Context()).URL

			shared.FlushResponse(w, func(w http.ResponseWriter) bool {
				query := u.Query()
				linkHeaders := make([]string, 0, len(sizes)-1)

				for _, size := range sizes[1:] {
					query.Set("sizes", fmt.Sprintf("%dx%d", size[0], size[1]))
					u.RawQuery = query.Encode()

					linkHeaders = append(linkHeaders, shared.BuildLinkHeaderFromStr(u.String(), "/optimux/"))
				}

				// Combine Link headers in a single call
				w.Header().Add("Link", strings.Join(linkHeaders, ", "))
				return true
			})
		}

		log.Println("processing completed")
	}
}

type DocsResponse struct {
	Path    string            `json:"path"`
	Params  map[string]any    `json:"params"`
	Headers map[string]string `json:"headers,omitempty"`
	Example []string          `json:"example,omitempty"`
}

func DocsHandler(quality int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		w.WriteHeader(http.StatusOK)
		resizeDocs := &DocsResponse{
			Path: "/optimux/resize",
			Params: map[string]any{
				"image_url": "string, required",
				"sizes":     "string, required, format: _width_x_height, example: 300x300, 300x0",
				"format":    "string, optional, options: jpeg,jpg,web, default: jpeg",
				"quality":   fmt.Sprintf("int, optional, options: 1..80, default: %d, example: 60", quality),
				"encoder":   "string, optional, options: json,stream,progress, default: stream.",
			},
			Example: []string{
				"/resize?image_url=https://image.url&sizes=600x0&quality=60&format=jpeg",
				"/resize?image_url=https://image_url&sizes=300x0&quality=60&format=webp&encoder=progress",
			},
		}

		assetsDocs := &DocsResponse{
			Path: "/optimux/assets/*",
			Params: map[string]any{
				"sizes":   "string, required, format: _width_x_height, example: 300x300, 300x0",
				"format":  "string, optional, options: jpeg,jpg,web, default: jpeg",
				"quality": fmt.Sprintf("int, optional, options: 1..80, default: %d, example: 60", quality),
				"encoder": "string, optional, options: json,stream,progress, default: stream.",
			},
		}

		resp := []*DocsResponse{resizeDocs, assetsDocs}

		b, err := json.MarshalIndent(resp, " ", " ")
		if err != nil {
			w.Write([]byte(fmt.Sprintf("%v", resp)))
			return
		}

		w.Write(b)
	}
}
