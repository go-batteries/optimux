package mediahose

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/roverxio/optimux/src/shared"
)

type MediaProcessor interface {
	Process(ctx context.Context, job *Job) ([]byte, error)
}

type ImageProcessor struct {
	ImageLoader LoadImageStrategy
}

func (ip *ImageProcessor) Process(ctx context.Context, job *Job) ([]byte, error) {
	media, err := ip.ImageLoader.LoadImage(job)
	if err != nil {
		log.Println("Failed to load image:", err)
		return nil, err
	}

	buf, err := ProcessImageWithResponse(job.Ctx, media, job)
	if err != nil {
		log.Println("failed to process image", err)
	}
	return buf, err
}

type VideoProcessor struct {
	Client DownloadClient
}

func (ip *VideoProcessor) Process(ctx context.Context, job *Job) ([]byte, error) {
	byts, err := LoadVideoFromURLWithCache(ip.Client, ctx, job.ImagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch video. error %v", err)
	}

	buf, err := io.ReadAll(byts)
	if err != nil {
		return nil, err
	}

	defer byts.Close()

	// job.Encoder(buf, job.ImagePath, job.Format, job.Resp)
	return buf, nil
}

// ProcessImageWithResponse handles resizing synchronously inside HTTP request
func ProcessImageWithResponse(ctx context.Context, img *vips.ImageRef, job *Job) ([]byte, error) {
	defer shared.Bench(fmt.Sprintf("ProcessImageWith Response. Hob %s", job.ID))()

	format := job.Format

	// quality := job.Quality
	// encoder := job.Encoder

	defer img.Close()

	// Open image with vips
	select {
	case <-ctx.Done():
		log.Println("Request canceled: Skipping image processing")
		return nil, ctx.Err()
	default:
	}

	// sizes := job.Sizes
	// size := sizes[0]
	// width, height := size[0], size[1]

	// log.Println("processing for size, format", size, format)

	ins := shared.I()
	tags := shared.GetServerTags()

	var (
		err error
		buf []byte // Encode image
	)

	if format == ".webp" {
		buf, _, err = WebpProcessor(img, job)
	} else if format == ".png" {
		buf, _, err = PngProcessor(img, job)
	} else {
		buf, _, err = JpegProcessor(img, job)
	}

	if err != nil {
		log.Println("Error encoding image:", err)

		ins.Incr(shared.EventAssetProcessingFailed, tags, 1)
		return nil, fmt.Errorf("failed to do shit")
	}

	ins.Incr(shared.EventAssetProcessingSuccess, tags, 1)

	log.Println("image resized success fully")
	return buf, nil

	// key := fmt.Sprintf("%d_%d_%d.%s", width, height, quality, format)

	// Respond with JSON
	// waitStart := time.Now()
	// err = encoder(buf, key, format, w)
	// if err != nil {
	// 	log.Printf("encoder returned error. %v", err)
	// }
	//
	// log.Println("sending api response took for id", time.Since(waitStart), job.ID)
	//
	// return err
}
