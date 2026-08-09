package mediametadata

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-batteries/slicendice"
	"github.com/roverxio/optimux/src/shared"
)

type DistributionRequest struct {
	MediaURL    string             `json:"media_url" validate:"required"`
	MediaID     string             `json:"media_id" validate:"required"`
	MediaPath   string             `json:"-" validate:"-"`
	SizeFormats []*SizeFormatTuple `json:"formats"`
	Prority     int                `json:"priority"`
}

func (d *DistributionRequest) BackfillMediaID() {
	if d.MediaID != "" {
		return
	}

	fileName, _, ok := shared.ExplodeFileName(d.MediaURL)
	if ok {
		d.MediaID = fileName
	}
}

func BuildDistributionReqFromUrl(u *url.URL) (*DistributionRequest, error) {
	fileName, ext, ok := shared.ExplodeFileName(u.Path)
	if !ok {
		return nil, errors.New("dead")
	}

	sizeFormats := []*SizeFormatTuple{}

	requestFormat := u.Query().Get("format")

	if requestFormat == "" {
		requestFormat = ext
	}

	sizeParams, ok := u.Query()["sizes"]

	for _, size := range sizeParams {
		sizeFormats = append(sizeFormats, &SizeFormatTuple{
			Size:   size,
			Format: requestFormat,
		})
	}

	return &DistributionRequest{
		MediaID:     fileName,
		MediaPath:   u.Path,
		MediaURL:    u.String(),
		SizeFormats: sizeFormats,
	}, nil
}

type DistributionRequestOpts func(r *DistributionRequest)

func WithSizeFormats(sizeFormats ...*SizeFormatTuple) DistributionRequestOpts {
	return func(r *DistributionRequest) {
		r.SizeFormats = append(r.SizeFormats, sizeFormats...)
	}
}

func WithMediaURL(mediaURL string) DistributionRequestOpts {
	return func(r *DistributionRequest) {
		r.MediaURL = mediaURL
	}
}

func NewDistributionRequest(urlPath string, opts ...DistributionRequestOpts) (*DistributionRequest, error) {
	fileName, _, ok := shared.ExplodeFileName(urlPath)
	if !ok {
		return nil, errors.New("dead")
	}

	m := &DistributionRequest{
		MediaID:     fileName,
		SizeFormats: []*SizeFormatTuple{},
	}

	for _, opt := range opts {
		opt(m)
	}

	return m, nil
}

type DistributionResponse struct {
	MediaID   string             `json:"media_id"`
	MediaSrcs []*SizeFormatTuple `json:"srcset"`
}

func BuildDistributionResponse(
	baseURL *url.URL,
	requests []*DistributionRequest,
	metadatas []*MediaMetadata,
) ([]*DistributionResponse, error) {
	// - Divide requests into two sets
	// - For requests with matching media id in db response
	//   - If requested size and format matches db response schema size and format
	//   - Build the url appending sizeID before ext.
	// - For the rest
	//   - For each format+size combination, build optmux's dynamic resize url

	mediaMap := make(map[string]*MediaMetadata)
	for _, meta := range metadatas {
		if err := meta.Enrich(); err != nil {
			return nil, err
		}

		mediaMap[meta.MediaID] = meta
	}

	log.Println("metadatas", shared.Dumps(metadatas))

	responses := []*DistributionResponse{}

	for _, req := range requests {
		meta, exists := mediaMap[req.MediaID]
		if !exists || meta.MetadataSchema == nil {
			continue
		}

		response := &DistributionResponse{
			MediaID:   meta.MediaID,
			MediaSrcs: []*SizeFormatTuple{},
		}

		for _, reqSize := range req.SizeFormats {
			matched, ok := slicendice.Find(meta.MetadataSchema.Sizes, func(tuple *SizeFormatTuple, _ int) bool {
				return tuple.Size == reqSize.Size && tuple.Format == reqSize.Format
			})

			if ok < 0 {
				resizeDestination := filepath.Join("/optimux/assets", *matched.Destination)
				matched.Destination = &resizeDestination
			}

			response.MediaSrcs = append(response.MediaSrcs, matched)
		}

		responses = append(responses, response)
	}

	return responses, nil
}

func UpdateS3Headers(ctx context.Context, defaultBucket string, client *s3.Client, metadatas ...*MediaMetadata) {
	cancels := []context.CancelFunc{}

	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	for _, metadata := range metadatas {
		bucket := metadata.MetadataSchema.Bucket
		if bucket == "" {
			bucket = defaultBucket
		}

		key := metadata.MetadataSchema.OriginalPath
		thumbKey := filepath.Join(
			filepath.Dir(key),
			filepath.Base(key),
			".webp",
		)

		cx, cancel := context.WithTimeout(ctx, 30*time.Second)
		cancels = append(cancels, cancel)

		_, err := client.CopyObject(cx, &s3.CopyObjectInput{
			Bucket:            &bucket,
			Key:               &key,
			CopySource:        shared.ToPtr(fmt.Sprintf("%s/%s", bucket, key)),
			MetadataDirective: types.MetadataDirectiveReplace,
			Metadata: map[string]string{
				shared.S3HeaderProcessedMedia: metadata.ProcessedKeys(),
			},
		})
		if err != nil {
			log.Println("failed to update s3 meta", bucket, key, err)
			continue
		}

		_, err = client.CopyObject(cx, &s3.CopyObjectInput{
			Bucket:            &bucket,
			Key:               &key,
			CopySource:        shared.ToPtr(fmt.Sprintf("%s/%s", bucket, thumbKey)),
			MetadataDirective: types.MetadataDirectiveReplace,
			Metadata: map[string]string{
				shared.S3HeaderProcessedMedia: metadata.ProcessedKeys(),
			},
		})
		if err != nil {
			// warning
			log.Println("failed to update to s3 thumb meta", bucket, thumbKey, err)
			continue
		}

		log.Println("successfully uploaded to s3", bucket, key, thumbKey)
	}
}

func GetS3Headers(ctx context.Context, s3Client *s3.Client, bucket, key string) (*s3.HeadObjectOutput, error) {
	cx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	head, err := s3Client.HeadObject(cx, &s3.HeadObjectInput{
		Bucket: shared.ToPtr(bucket),
		Key:    shared.ToPtr(key),
	})
	if err != nil {
		log.Println("failed to get s3 headers", bucket, key, err)
		return nil, err
	}

	if head.ContentLength == nil {
		log.Println("could not determine content length for", bucket, key)
		return nil, errors.New("invalid content")
	}

	return head, nil
}
