package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-batteries/slicendice"
	"github.com/roverxio/optimux/src/config"
	"github.com/roverxio/optimux/src/encoders"
	"github.com/roverxio/optimux/src/mediahose"
	"github.com/roverxio/optimux/src/services/mediametadata"
	"github.com/roverxio/optimux/src/shared"
)

type BatchedMediaHandler struct {
	Catergory   string // s3,public,...
	Scaler      *mediahose.DynamicScaler[*mediahose.BatchedJob]
	Dispatcher  *mediahose.Dispatcher
	JobQ        chan *mediahose.BatchedJob
	MetadataSvc mediametadata.MetadataService
}

func (s *BatchedMediaHandler) Handle(quality int, cfg *config.Config, pathPrefix, replacePathPrefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer shared.Bench("batched media handler")()

		ctx := r.Context()

		query := r.URL.Query()

		uid := query.Get("uid")
		if uid == "" {
			log.Println("batch id missing in url", r.URL.String())

			http.Error(w, "must have a batch id", http.StatusBadRequest)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, pathPrefix)
		if path == "" {
			http.Error(w, "Invalid URL", http.StatusNotFound)
			return
		}

		s3BaseUrl := cfg.S3BaseURL
		s3URL, _ := url.Parse(s3BaseUrl) // we have already validated this at app start
		s3URL = s3URL.JoinPath(replacePathPrefix, path)

		imagePath := s3URL.String()
		if imagePath == "" || imagePath == s3BaseUrl {
			log.Println("missing image url", s3BaseUrl, s3URL.String(), imagePath)

			http.Error(w, "Missing image URL", http.StatusBadRequest)
			return
		}

		log.Println("s3 path", imagePath)

		// id := shared.CalculateHash(imagePath)
		fileName, _, ok := shared.ExplodeFileName(imagePath)
		if !ok {
			log.Println("invalid media url for imagePath", imagePath)
			http.Error(w, "invalid media url", http.StatusBadRequest)

			return
		}

		derivedFormat := filepath.Ext(imagePath)

		format := query.Get("format")
		if format == "" {
			format = derivedFormat
		} else {
			format = fmt.Sprintf(".%s", format)
		}

		// if format != "jpeg" && format != "webp" {
		// 	http.Error(w, "Invalid format", http.StatusBadRequest)
		// 	return
		// }

		var sizes [][2]int

		sizesParam, _ := query["sizes"]
		sizes = shared.SanitizeSizes(sizesParam)

		skipResize := false

		shared.TransformSizesForWorker(sizesParam)
		sizeFormats := slicendice.Map(sizesParam, func(sizeParam string, _ int) *mediametadata.SizeFormatTuple {
			return &mediametadata.SizeFormatTuple{Size: sizeParam, Format: format}
		})
		v1Schema := &mediametadata.V1MedatataSchema{
			OriginalPath: path,
			Sizes:        sizeFormats,
		}

		results := []*mediametadata.MediaMetadata{}
		metadata, err := mediametadata.BuildMediaMetadata(path,
			mediametadata.WithV1MetadataSchema(v1Schema))
		if err == nil {
			results, err = s.MetadataSvc.FetchPreComputedFromMetadata(ctx, metadata)
		}

		if err != nil {
			log.Println("failed to get media metadata info. skipping to usual dynamic flow", err)
		}

		skipResize = err == nil && len(results) > 0

		qualityParams := query.Get("quality")
		if qualityParams != "" {
			q, err := strconv.Atoi(qualityParams)
			if err == nil {
				quality = shared.ClampValue(0, 100, q)
			}
		}

		log.Println("quality requested", quality)

		encoderParams := query.Get("encoder")
		if encoderParams == "" {
			encoderParams = "stream"
		}

		if len(sizes) == 0 && quality == 100 {
			skipResize = true
			// http.Error(w, "Invalid sizes, use 'sizes=800x600,400x0,0x0'", http.StatusBadRequest)
			// return
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

		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if skipResize && len(results) > 0 {
			for _, result := range results {
				for _, size := range result.MetadataSchema.Sizes {
					if size.Size == sizesParam[0] {
						imagePath = fmt.Sprintf("%s/%s", cfg.S3BaseURL, *size.Destination)

						log.Println("final image path", imagePath)
					}
				}
			}
		}

		log.Println("is size pre-coumputed?", skipResize, fileName)

		event := shared.EventAssetNotFound
		if skipResize {
			event = shared.EventAssetFound
		}

		shared.I().Incr(event, shared.GetServerTags(), 1)

		job := &mediahose.Job{
			ID:              fileName,
			ImagePath:       imagePath,
			Format:          format,
			Sizes:           sizes,
			Quality:         quality,
			Resp:            w,
			Ctx:             ctx,
			CancelCtx:       cancel,
			Done:            make(shared.DoneCh),
			Encoder:         encoder,
			ErrHandler:      shared.ResponseWriter,
			MediaType:       mediaType,
			ImageLoader:     strategy,
			OrigPath:        r.URL.String(),
			SkipResize:      skipResize,
			MailBox:         make(shared.MailBoxCh, 1),
			DefaultS3Bucket: cfg.DefaultS3Bucket,
		}

		select {
		case <-time.After(shared.DefaultWaitTillEnQTime):
			http.Error(w, "Server too busy", http.StatusServiceUnavailable)
			return
		default: // TODO: fix this, this is shit logic, way to solve this is
			// a request-response model, Add() returns a channel
			// we wait on channel
			s.Dispatcher.Add(job.Ctx, uid, job)

			queueUsage := float64(len(s.JobQ)) / float64(cap(s.JobQ))
			if queueUsage > 0.75 && s.Scaler.ActiveCount() < s.Scaler.MaxWorkers {
				log.Printf("⚠️  Batcher Queue is at %.2f%% capacity, scaling up workers!", queueUsage*100)

				s.Scaler.ScaleSigChan <- struct{}{}
			}

			if len(sizes) > 1 {
				u := r.Clone(r.Context()).URL
				u.Host = cfg.Domain.Host
				u.Scheme = cfg.Domain.Scheme

				shared.FlushResponse(w, func(w http.ResponseWriter) bool {
					query := u.Query()
					linkHeaders := make([]string, 0, len(sizes)-1)

					for _, size := range sizes[1:] {
						sizeStr, ok := shared.ToSizeStr(size[0], size[1])
						if ok {
							query.Set("sizes", sizeStr)
							u.RawQuery = query.Encode()
						}

						linkHeaders = append(
							linkHeaders,
							shared.BuildLinkHeaderFromStr(u.String(), "/optimux/"),
						)
					}

					// Combine Link headers in a single call
					w.Header().Add("Link", strings.Join(linkHeaders, ", "))
					return true
				})
			}
		}

		<-job.Done

		job.MailBox = nil

		log.Println("processing completed")
	}
}
