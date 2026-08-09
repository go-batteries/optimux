package energon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-batteries/slicendice"
	"github.com/roverxio/optimux/src/encoders"
	"github.com/roverxio/optimux/src/mediahose"
	"github.com/roverxio/optimux/src/services/mediametadata"
	"github.com/roverxio/optimux/src/shared"
)

type ImageProcessorHandler struct {
	S3Client   *s3.Client
	Worker     *mediahose.BatchWorker
	BatchQueue chan *mediahose.BatchedJob
	Encoder    encoders.Encoder
	Dispatcher *mediahose.Dispatcher
}

type LambdaRecord struct {
	Bucket          string
	UploadedPath    string
	RequestedFormat string
	Region          string
	BatchID         string
	Ctx             context.Context
	Cancel          context.CancelFunc
}

func (ph *ImageProcessorHandler) Handle(ctx context.Context, record *LambdaRecord) ([]shared.MailBoxCh, error) {
	// worker := &mediahose.BatchWorker{
	// 	Idx:        time.Now().UTC().Unix(),
	// 	CloserChan: make(chan int64),
	// }

	// log.Printf("transformed record %+v", *record)

	uploadedAsset, format := filepath.Base(record.UploadedPath), filepath.Ext(record.UploadedPath)
	dir := strings.TrimPrefix(filepath.Dir(record.UploadedPath), "/")

	log.Println("processing asset", uploadedAsset)

	// Skip if generated assert is not an `img` type (prefix)
	if !strings.HasPrefix(uploadedAsset, shared.MediaPrefixImage) {
		return nil, errors.New("invalid_generated_media")
	}

	isImage := shared.IsOfMediaType(format, shared.AllowedImgExtMap)
	isVideo := shared.IsOfMediaType(format, shared.VideoExtMap)

	if !(isImage || isVideo) {
		log.Printf("%s is neither an image or video. moving on", uploadedAsset)
		return nil, errors.New("invalid_media_type")
	}

	// TODO: using HTTP path, but will need to switch to Aws SDK S3 Client Download
	imagePath := filepath.Join(
		fmt.Sprintf("https://%s.s3.%s.amazonaws.com/", record.Bucket, record.Region),
		record.UploadedPath,
	)
	// - Validate that the file name begins with the prefix img_
	// - Each record / image needs to be downloaded , reszied into 3 sizes and uploaded back

	// computedSizes := shared.SanitizeSizes(shared.SizesForCompression)

	sizeMap := map[string][2]int{}

	computedSizes := slicendice.Reduce(
		shared.SizesForCompression,
		func(acc map[string][2]int, el string, _ int) map[string][2]int {
			sizeStr, ok := shared.IdSizeMap[el]
			if !ok {
				return acc
			}

			acc[el] = shared.SanitizeSizes([]string{sizeStr})[0]
			return acc
		},
		sizeMap,
	)

	dones := []shared.MailBoxCh{}

	// var wg sync.WaitGroup
	// wg.Add(len(computedSizes))

	requestedFormat := format
	if record.RequestedFormat != "" {
		requestedFormat = record.RequestedFormat
	}

	for sizeId, size := range computedSizes {
		fileName := strings.TrimSuffix(filepath.Base(uploadedAsset), format)
		fileNameWithSize := fmt.Sprintf("%s_%s%s", fileName, sizeId, requestedFormat)
		recordKey := shared.GetResizedS3Key(dir, fileNameWithSize)
		skipUpload := false

		if _, err := mediametadata.GetS3Headers(ctx, ph.S3Client, record.Bucket, recordKey); err == nil {
			log.Println(recordKey, "image already post processed. skipping")
			skipUpload = true
		}

		job := &mediahose.Job{
			ID:        fileName,
			ImagePath: record.UploadedPath,
			Format:    requestedFormat,
			Sizes:     [][2]int{size},
			Quality:   80,
			Ctx:       context.Background(),
			CancelCtx: func() {},
			Done:      make(shared.DoneCh, 1),
			Encoder:   ph.Encoder,
			ErrHandler: func(w io.Writer, msg string, code int) {
				log.Println("failed to process image", msg, "for image path", imagePath)
			},
			MediaType: mediahose.MediaTypeImage,
			ImageLoader: mediahose.NewMediaLoader(
				mediahose.NewS3BasicClient(
					ph.S3Client,
					record.Bucket,
					record.Region, // This is expected to be a homogenous values. Values have been filtered by region
				),
			),
			OrigPath:        imagePath,
			S3Bucket:        shared.ToPtr(record.Bucket),
			S3Key:           shared.ToPtr(recordKey),
			SkipResize:      false,
			SkipUpload:      skipUpload,
			MailBox:         make(shared.MailBoxCh, 1),
			DefaultS3Bucket: record.Bucket,
		}

		ph.Dispatcher.Add(ctx, record.BatchID, job)
		dones = append(dones, job.MailBox)
	}

	log.Println("🎟️ queued job", len(dones), dones)
	return dones, nil
}

func generateObjectPathForSize(uploadPath string, sizeID string) string {
	ext := filepath.Ext(uploadPath)
	fileName := strings.TrimSuffix(filepath.Base(uploadPath), ext)

	log.Println(
		"uploadPath", uploadPath,
		"sizeID", sizeID,
		"file", fileName,
		"ext", ext,
	)

	return fmt.Sprintf("%s_%s%s", fileName, sizeID, ext)
}
