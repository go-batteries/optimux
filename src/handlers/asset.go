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

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-batteries/slicendice"
	"github.com/go-batteries/optimux/src/config"
	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/mediahose"
	"github.com/go-batteries/optimux/src/services/mediametadata"
	"github.com/go-batteries/optimux/src/shared"
)

type S3ProxyImageHandler struct {
	JobQ        chan *mediahose.Job
	Scaler      *mediahose.DynamicScaler[*mediahose.Job]
	MetadataSvc mediametadata.MetadataService
}

func (s *S3ProxyImageHandler) Handle(quality int, cfg *config.Config, pathPrefix, replacePathPrefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer shared.Bench("s3 image resize handler")()

		ctx := r.Context()
		query := r.URL.Query()
		// linkHeader := BuildLinkHeader(r.URL)

		s3BaseUrl := cfg.S3BaseURL

		path := strings.TrimPrefix(r.URL.Path, pathPrefix)
		if path == "" {
			http.Error(w, "Invalid URL", http.StatusNotFound)
			return
		}

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

		var sizes [][2]int

		sizesParam, _ := query["sizes"]
		sizesParam = slicendice.ToSet(sizesParam...).ToList() // equivalent to `list | uniq`

		sizes = shared.SanitizeSizes(sizesParam)

		// convert the incoming sizes to support for,
		// worker processed media size key.
		// check if metadata exists and size available for format
		// set job.SkipResize = true

		skipResize := false

		shared.TransformSizesForWorker(sizesParam)
		// sizeFormats := slicendice.Map(sizesParam, func(sizeParam string, _ int) *mediametadata.SizeFormatTuple {
		// 	return &mediametadata.SizeFormatTuple{Size: sizeParam, Format: format}
		// })
		// v1Schema := &mediametadata.V1MedatataSchema{
		// 	OriginalPath: path,
		// 	Sizes:        sizeFormats,
		// }
		//
		// results := []*mediametadata.MediaMetadata{}
		//
		// metadata, err := mediametadata.BuildMediaMetadata(path,
		// 	mediametadata.WithV1MetadataSchema(v1Schema))
		// if err == nil {
		// 	results, err = s.MetadataSvc.FetchPreComputedFromMetadata(ctx, metadata)
		// }
		//
		// if err != nil {
		// 	log.Println("failed to get media metadata info. skipping to usual dynamic flow", err)
		// }

		awscfg, err := awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithDefaultRegion(cfg.AwsRegion))
		if err != nil {
			log.Fatal("failed to initialize config")
		}

		s3Client := s3.NewFromConfig(awscfg)
		var processedMediasInHeader string

		bucket, key, ok := shared.ExtractBucketAndKeyFromS3(imagePath)

		headRes, err := mediametadata.GetS3Headers(ctx, s3Client, bucket, key)
		if err == nil {
			result, ok := headRes.Metadata[shared.S3HeaderProcessedMedia]
			if !ok || len(result) == 0 {
				log.Println("no", shared.S3HeaderProcessedMedia, "in s3 metadata")
				ok = false // for the results len, we reset
			}

			processedMediasInHeader, skipResize = result, ok

		} else {
			log.Println("failed to get s3 headers", err)
			skipResize = false
		}

		// TODO: On SkipResize,
		// Construct an S3Event and
		// Push to SQS

		if len(sizes) == 0 {
			log.Println("invalid sizes", r.URL.String())

			http.Error(w, "Invalid sizes, use 'sizes=800x600,400x0,0x0'", http.StatusBadRequest)
			return
		}

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

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		for _, size := range sizesParam {
			key := fmt.Sprintf("%s%s", size, format)

			log.Println("matching", processedMediasInHeader, key)
			if strings.Contains(processedMediasInHeader, key) {
				skipResize = true
				break
			}
		}

		log.Println("is size pre-coumputed?", skipResize, fileName)

		event := shared.EventAssetNotFound
		if skipResize {
			event = shared.EventAssetFound
		}

		shared.I().Incr(event, shared.GetServerTags(), 1)

		if skipResize {
			s3URL.Path = shared.ReplaceWithResizedURL(s3URL.Path, sizesParam[0])
			imagePath = s3URL.String()

			log.Println("changed image path", imagePath)
		}

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
		case s.JobQ <- job:
			queueUsage := float64(len(s.JobQ)) / float64(cap(s.JobQ))
			if queueUsage > 0.75 && s.Scaler.ActiveCount() < s.Scaler.MaxWorkers {
				log.Printf("⚠️  Queue is at %.2f%% capacity, scaling up workers!", queueUsage*100)
				select {
				case s.Scaler.ScaleSigChan <- struct{}{}:
				default:
				}
			}

		case <-time.After(shared.DefaultWaitTillEnQTime):
			http.Error(w, "Server too busy", http.StatusServiceUnavailable)
			return
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

					linkHeaders = append(linkHeaders, shared.BuildLinkHeaderFromStr(u.String(), "/optimux/"))
				}

				// Combine Link headers in a single call
				w.Header().Add("Link", strings.Join(linkHeaders, ", "))
				return true
			})
		}

		<-job.Done
		log.Println("processing completed")
	}
}
