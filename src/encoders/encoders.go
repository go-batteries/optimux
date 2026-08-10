package encoders

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-batteries/optimux/src/shared"
)

// Image processing request structure
type Result struct {
	Data map[string]string `json:"data"`
}

type ResponseOpts struct {
	Filename string
	Format   string

	S3Bucket *string
	S3Key    *string
	Data     []byte

	SkipUpload bool
}

// TODO: Change the name from Encoder to ResponseHandler/ResponseWriter
// / Encoders
type Encoder func(ctx context.Context, opts *ResponseOpts, w io.Writer) error

func Base64Encoder(ctx context.Context, opts *ResponseOpts, w io.Writer) error {
	data, fileName := opts.Data, opts.Filename

	rw := w.(http.ResponseWriter) // TODO: use type assertion

	encoded := base64.StdEncoding.EncodeToString(data)
	response := map[string]string{
		fileName: fmt.Sprintf("data:image;base64,%s", encoded),
	}

	rw.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(rw).Encode(Result{Data: response})
}

func StreamEncoder(ctx context.Context, opts *ResponseOpts, w io.Writer) (err error) {
	data, fileName := opts.Data, opts.Filename

	log.Printf("🔥 StreamEncoder: Called with format='%s', filename='%s', data_len=%d", opts.Format, fileName, len(data))

	rw := w.(http.ResponseWriter) // TODO: use type assertion

	contentType, err := shared.ContentTypeForFormat(opts.Format)
	if err != nil {
		log.Printf("🔥 StreamEncoder: ContentTypeForFormat FAILED: %v", err)
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return err
	}
	
	log.Printf("🔥 StreamEncoder: Got contentType='%s'", contentType)

	rw.Header().Set("Content-Type", contentType)
	rw.Header().Set("Transfer-Encoding", "chunked")
	// w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	etag := shared.CalculateHash(fileName)
	rw.Header().Set("Etag", fmt.Sprintf(`"%s"`, etag))

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	// Write data in chunks
	chunkSize := 4096
	for start := 0; start < len(data); start += chunkSize {
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}

		// Write chunk to response
		_, err := w.Write(data[start:end])
		if err != nil {
			return fmt.Errorf("failed to write chunk: %v", err)
		}

		// Flush immediately to send data to the client
		flusher.Flush()
	}

	return err
}

func ProgressiveStreamEncoder(ctx context.Context, opts *ResponseOpts, w io.Writer) (err error) {
	data, fileName := opts.Data, opts.Filename

	rw := w.(http.ResponseWriter) // TODO: use type assertion

	contentType, err := shared.ContentTypeForFormat(opts.Format)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return err
	}

	rw.Header().Set("Content-Type", contentType)
	etag := shared.CalculateHash(fileName)

	rw.Header().Set("Etag", fmt.Sprintf(`"%s"`, etag))
	rw.Header().Set("Transfer-Encoding", "chunked")

	flusher, ok := rw.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	// https://blog.cloudflare.com/parallel-streaming-of-progressive-images/
	// probably
	// Define chunk sizes based on priority
	headerSize := 1024  // Smallest: Send immediately (high priority)
	previewSize := 8192 // Medium priority (small preview)
	chunkSize := 32768  // Standard chunk size for remainder (low priority)

	// Step 1: Send Image Header (High Priority)
	if len(data) > headerSize {
		_, err := rw.Write(data[:headerSize])
		if err != nil {
			return fmt.Errorf("failed to write image header: %v", err)
		}
		flusher.Flush()
	}

	// Step 2: Send Preview Data (Medium Priority)
	if len(data) > previewSize {
		_, err := rw.Write(data[headerSize:previewSize])
		if err != nil {
			return fmt.Errorf("failed to write preview data: %v", err)
		}
		flusher.Flush()
	}

	// Step 3: Send Remaining Image Data in Chunks (Low Priority)
	for start := previewSize; start < len(data); start += chunkSize {
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}

		_, err := rw.Write(data[start:end])
		if err != nil {
			return fmt.Errorf("failed to write remaining image data: %v", err)
		}

		flusher.Flush()
	}

	return nil
}

type S3Uploader struct {
	client *s3.Client
}

func NewS3Uploader(client *s3.Client) *S3Uploader {
	return &S3Uploader{client}
}

func (up *S3Uploader) Upload(ctx context.Context, opt *ResponseOpts, _ io.Writer) (err error) {
	// log.Printf("uploading to s3. %+v", shared.Dumps(opt))
	if opt.SkipUpload {
		return nil
	}

	imageBuffer := bytes.NewReader(opt.Data)

	format, ok := shared.AllowedImgExtMap[opt.Format]
	if !ok {
		return fmt.Errorf("unsupported file format %s", opt.Format)
	}

	if opt.S3Bucket == nil || opt.S3Key == nil {
		log.Println("error: bucket or key is nil")
		return fmt.Errorf("invalid destination options")
	}

	log.Println("uploading to s3", *opt.S3Bucket, *opt.S3Key, "skip?", opt.SkipUpload)
	_, err = up.client.PutObject(
		ctx,
		&s3.PutObjectInput{
			Bucket:      opt.S3Bucket,
			Key:         opt.S3Key,
			Body:        imageBuffer,
			ContentType: shared.ToPtr(format),
			Tagging:     shared.ToPtr("processed=true"),
		},
	)
	if err != nil {
		log.Printf("failed to upload to S3. error %v", err)
		return fmt.Errorf("failed to upload to s3. generated path s3://%v/%v. error: %v", opt.S3Bucket, opt.S3Key, err)
	}

	log.Println("uploaded successfully", opt.Format, opt.Filename)

	return nil
}
