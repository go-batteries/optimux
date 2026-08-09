package main

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "net/http/pprof"

	"github.com/davidbyttow/govips/v2/vips"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

// Benchmark function for logging execution time
func Bench(msg string) func() {
	start := time.Now()
	return func() {
		log.Printf("%s took %v", msg, time.Since(start))
	}
}

// Image processing request structure
type Result struct {
	Data map[string]string `json:"data"`
}

// ProcessImage handles resizing synchronously inside HTTP request
func FetchNProcessImage(ctx context.Context, id, imagePath string, sizes [][2]int, format string, quality int, w http.ResponseWriter) error {
	defer Bench(fmt.Sprintf("process image %s", id))()

	// Open image with vips
	select {
	case <-ctx.Done():
		log.Println("Request canceled: Skipping image processing")
		return ctx.Err()
	default:
	}

	img, err := LoadImageFromURLWithCache(imagePath)
	if err != nil {
		return fmt.Errorf("failed to load image. err %v", err)
	}
	defer img.Close()

	originalWidth := img.Width()
	originalHeight := img.Height()

	results := make(map[string]string)

	for _, size := range sizes {
		width, height := size[0], size[1]

		select {
		case <-ctx.Done():
			log.Println("Request canceled: Stopping processing for", width, "x", height)
			return ctx.Err()
		default:
		}

		// Preserve aspect ratio
		if width > 0 && height == 0 {
			height = (width * originalHeight) / originalWidth
		} else if height > 0 && width == 0 {
			width = (height * originalWidth) / originalHeight
		}

		// Resize image
		resizedImg, err := img.Copy()
		if err != nil {
			log.Println("Error copying image:", err)
			continue
		}
		defer resizedImg.Close()

		resizedImg.Resize(float64(width)/float64(originalWidth), vips.KernelLinear) // vips.KernelLanczos2

		// Encode image
		var buf []byte
		if format == "webp" {
			params := vips.NewWebpExportParams()
			params.Quality = quality
			buf, _, err = resizedImg.ExportWebp(params)
		} else {
			params := vips.NewJpegExportParams()
			params.Quality = quality
			params.Interlace = true
			buf, _, err = resizedImg.ExportJpeg(params)
		}
		if err != nil {
			log.Println("Error encoding image:", err)
			continue
		}

		encoded := base64.StdEncoding.EncodeToString(buf)
		key := fmt.Sprintf("%d_%d", width, height)
		results[key] = fmt.Sprintf("data:image/%s;base64, %s", format, encoded)
	}

	// Respond with JSON
	waitStart := time.Now()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Result{Data: results})
	log.Println("sending api response took for id", time.Since(waitStart), id)

	return nil
}

func ProcessImage(ctx context.Context, id string, img *vips.ImageRef, sizes [][2]int, format string, quality int, w http.ResponseWriter) error {
	defer Bench(fmt.Sprintf("process image %s", id))()
	defer img.Close()

	// Open image with vips
	select {
	case <-ctx.Done():
		log.Println("Request canceled: Skipping image processing")
		return ctx.Err()
	default:
	}

	// img, err := LoadImageFromURLWithCache(imagePath)
	// if err != nil {
	// 	return fmt.Errorf("failed to load image. err %v", err)
	// }

	originalWidth := img.Width()
	originalHeight := img.Height()

	results := make(map[string]string)

	for _, size := range sizes {
		width, height := size[0], size[1]

		select {
		case <-ctx.Done():
			log.Println("Request canceled: Stopping processing for", width, "x", height)
			return ctx.Err()
		default:
		}

		// Preserve aspect ratio
		if width > 0 && height == 0 {
			height = (width * originalHeight) / originalWidth
		} else if height > 0 && width == 0 {
			width = (height * originalWidth) / originalHeight
		}

		// Resize image
		resizedImg, err := img.Copy()
		if err != nil {
			log.Println("Error copying image:", err)
			return fmt.Errorf("error copying image. error %v", err)
		}
		defer resizedImg.Close()

		resizedImg.Resize(float64(width)/float64(originalWidth), vips.KernelLinear) // vips.KernelLanczos2

		// Encode image
		var buf []byte
		if format == "webp" {
			params := vips.NewWebpExportParams()
			params.Quality = quality
			buf, _, err = resizedImg.ExportWebp(params)
		} else {
			params := vips.NewJpegExportParams()
			params.Quality = quality
			buf, _, err = resizedImg.ExportJpeg(params)
		}
		if err != nil {
			log.Println("Error encoding image:", err)
			return fmt.Errorf("failed to do shit")
		}

		encoded := base64.StdEncoding.EncodeToString(buf)
		key := fmt.Sprintf("%d_%d", width, height)
		results[key] = fmt.Sprintf("data:image/%s;base64, %s", format, encoded)
	}

	// Respond with JSON
	waitStart := time.Now()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Result{Data: results})
	log.Println("sending api response took for id", time.Since(waitStart), id)

	return nil
}

// Image caching
const tmpfsCacheDir = "/tmp/shm/image_cache"

func calculateHash(str string) string {
	hash := md5.Sum([]byte(str))
	return hex.EncodeToString(hash[:])
}

func getCacheFilePath(imageURL string) string {
	hash := md5.Sum([]byte(imageURL))
	filename := hex.EncodeToString(hash[:]) + ".jpg"
	return filepath.Join(tmpfsCacheDir, filename)
}

func LoadImageFromURLWithCache(imageURL string) (*vips.ImageRef, error) {
	if err := os.MkdirAll(tmpfsCacheDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create tmpfs cache dir: %v", err)
	}

	cachePath := getCacheFilePath(imageURL)

	if _, err := os.Stat(cachePath); err == nil {
		return LoadImageFromTmpFS(cachePath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %v", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %v", err)
	}
	defer resp.Body.Close()

	file, err := os.Create(cachePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create tmpfs cache file: %v", err)
	}
	defer file.Close()

	// var buf bytes.Buffer
	// multiWriter := io.MultiWriter(file, &buf)
	// if _, err := io.Copy(multiWriter, resp.Body); err != nil {
	// 	return nil, fmt.Errorf("failed to write image to tmpfs: %v", err)
	// }
	//
	// return vips.NewImageFromReader(bytes.NewReader(buf.Bytes()))
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to write image to tmpfs: %v", err)
	}

	_, err = file.Write(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to write image to tmpfs: %v", err)
	}

	return vips.NewImageFromBuffer(buf)
}

func LoadImageFromTmpFS(imagePath string) (*vips.ImageRef, error) {
	// ref, err := os.ReadFile(imagePath)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to read image from tmpfs: %v", err)
	// }
	ref, err := vips.NewImageFromFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image from tmpfs: %v", err)
	}

	return ref, nil
	// return vips.NewImageFromBuffer(buf)
}

// ImageHandler processes images directly (No Dispatcher)
func ImageHandler(quality int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		imagePath := "./images/sample.jpg"
		if val, ok := query["image_url"]; ok {
			imagePath = val[0]
		}

		id := calculateHash(imagePath)
		defer Bench(fmt.Sprintf("image handler %s", id))()

		format := query.Get("format")
		if format != "jpeg" && format != "webp" {
			http.Error(w, "Invalid format, use 'jpeg' or 'webp'", http.StatusBadRequest)
			return
		}

		// Parse sizes (comma-separated: 800x600,400x0)
		sizes := [][2]int{}
		if sizesParam, ok := query["sizes"]; ok {
			for _, size := range sizesParam {
				var width, height int
				fmt.Sscanf(size, "%dx%d", &width, &height)
				if width > 0 || height > 0 {
					sizes = append(sizes, [2]int{width, height})
				}
			}
		}

		if len(sizes) == 0 {
			http.Error(w, "Invalid sizes, use 'sizes=800x600,400x0'", http.StatusBadRequest)
			return
		}

		qualityParams := query.Get("quality")
		if qualityParams != "" {
			q, err := strconv.Atoi(qualityParams)
			if err == nil && q > 0 && q < 81 {
				quality = q
			}
		}
		FetchNProcessImage(r.Context(), id, imagePath, sizes, format, quality, w)
	}
}

// Job represents an image processing task
type Job struct {
	ID        string
	ImagePath string
	Format    string   // "jpeg" or "webp"
	Sizes     [][2]int // List of width-height pairs
	Quality   int
	Resp      http.ResponseWriter
	Done      chan struct{}
	Ctx       context.Context
}

var fetchQueue = make(chan Job, 20)

// processorQueue = make(chan string, 20)

func FetchWorker(ctx context.Context) {
	go func(cx context.Context) {
		for {
			select {
			case <-ctx.Done():
				log.Println("Signal to exit fetcher")
				return

			case job := <-fetchQueue:
				img, err := LoadImageFromURLWithCache(job.ImagePath)
				if err != nil {
					log.Println("Failed to load image:", err)
					continue
				}

				// Process the image in parallel
				err = ProcessImage(job.Ctx, job.ID, img, job.Sizes, job.Format, job.Quality, job.Resp)
				if err != nil {
					http.Error(job.Resp, err.Error(), http.StatusInternalServerError)
				}
				close(job.Done)
			}
		}
		// for job := range fetchQueue {
		// 	img, err := LoadImageFromURLWithCache(job.ImagePath)
		// 	if err != nil {
		// 		log.Println("Failed to load image:", err)
		// 		continue
		// 	}
		//
		// 	// Process the image in parallel
		// 	ProcessImage(job.Ctx, job.ID, img, job.Sizes, job.Format, job.Quality, job.Resp)
		// }
	}(ctx)
}

// ImageHandlerAsync processes images directly (No Dispatcher)
func ImageHandlerAsync(quality int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		imagePath := "./images/sample.jpg"
		if val, ok := query["image_url"]; ok {
			imagePath = val[0]
		}

		id := calculateHash(imagePath)
		defer Bench(fmt.Sprintf("image handler %s", id))()

		format := query.Get("format")
		if format != "jpeg" && format != "webp" {
			http.Error(w, "Invalid format, use 'jpeg' or 'webp'", http.StatusBadRequest)
			return
		}

		// Parse sizes (comma-separated: 800x600,400x0)
		sizes := [][2]int{}
		if sizesParam, ok := query["sizes"]; ok {
			for _, size := range sizesParam {
				var width, height int
				fmt.Sscanf(size, "%dx%d", &width, &height)
				if width > 0 || height > 0 {
					sizes = append(sizes, [2]int{width, height})
				}
			}
		}

		if len(sizes) == 0 {
			http.Error(w, "Invalid sizes, use 'sizes=800x600,400x0'", http.StatusBadRequest)
			return
		}

		qualityParams := query.Get("quality")
		if qualityParams != "" {
			q, err := strconv.Atoi(qualityParams)
			if err == nil && q > 0 && q < 81 {
				quality = q
			}
		}
		job := Job{
			ID:        id,
			ImagePath: imagePath,
			Format:    format,
			Sizes:     sizes,
			Quality:   quality,
			Resp:      w,
			Ctx:       r.Context(),
			Done:      make(chan struct{}),
		}

		select {
		case fetchQueue <- job:
		default:
			http.Error(w, "Server too busy", http.StatusServiceUnavailable)
			return
		}

		<-job.Done
		log.Println("processing completed")
		return
	}
}

// Main function
func main() {
	var port string
	var quality int

	flag.StringVar(&port, "port", ":8811", "port number")
	flag.IntVar(&quality, "quality", 80, "image quality")
	flag.Parse()

	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(5)

	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	file, err := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	log.SetOutput(file)
	log.SetFlags(log.Lshortfile)

	vips.Startup(&vips.Config{ConcurrencyLevel: 20})
	defer vips.Shutdown()

	go func() {
		fmt.Println("pprof running on :6060")
		log.Println(http.ListenAndServe(":6060", nil))
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGABRT, syscall.SIGKILL)
	defer stop()

	FetchWorker(ctx)
	FetchWorker(ctx)
	FetchWorker(ctx)
	FetchWorker(ctx)

	http.HandleFunc("/resize", ImageHandlerAsync(quality))

	fmt.Println("Server running on", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
