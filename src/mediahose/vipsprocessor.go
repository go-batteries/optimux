package mediahose

import (
	"log"

	"github.com/davidbyttow/govips/v2/vips"
)

type ImageExporter func(*vips.ImageRef, *Job) ([]byte, *vips.ImageMetadata, error)

func WebpProcessor(img *vips.ImageRef, job *Job) ([]byte, *vips.ImageMetadata, error) {
	log.Println("---webp", job.ID, job.Sizes)

	params := vips.NewWebpExportParams()
	params.Quality = job.Quality
	return img.ExportWebp(params)
}

func JpegProcessor(img *vips.ImageRef, job *Job) ([]byte, *vips.ImageMetadata, error) {
	log.Println("---jpeg", job.ID, job.Sizes)

	params := vips.NewJpegExportParams()
	params.Interlace = true
	params.Quality = job.Quality
	return img.ExportJpeg(params)
}

func PngProcessor(img *vips.ImageRef, job *Job) ([]byte, *vips.ImageMetadata, error) {
	log.Println("---png", job.ID, job.Sizes)

	params := vips.NewPngExportParams()
	params.Quality = job.Quality
	params.StripMetadata = true
	// params.Interlace = true

	return img.ExportPng(params)
}
