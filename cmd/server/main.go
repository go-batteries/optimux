package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/davidbyttow/govips/v2/vips"

	appcfg "github.com/go-batteries/optimux/src/config"
	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/ffmpeg"
	"github.com/go-batteries/optimux/src/handlers"
	"github.com/go-batteries/optimux/src/mediahose"
	"github.com/go-batteries/optimux/src/shared"
	"golang.org/x/net/http2"

	_ "github.com/lib/pq"
)

func main() {
	cfg := appcfg.LoadFromCliArgs(appcfg.WithEnvConfigLoaderOpts())

	log.Println("using qsize", cfg.QSize)

	// Initialize AWS S3 client for video sprite handling
	awsCfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(cfg.AwsRegion))
	if err != nil {
		log.Printf("Failed to load AWS config: %v", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)

	var logWriter io.Writer = os.Stdout

	if len(cfg.LogFile) > 0 {
		log.Println("logging to file: ", cfg.LogFile)

		file, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		logWriter = file
	}

	log.SetOutput(logWriter)

	log.SetFlags(log.Lshortfile)

	vips.Startup(&vips.Config{ConcurrencyLevel: int(cfg.VipsConcurrency)})
	defer vips.Shutdown()

	if cfg.DoProfile {
		runtime.SetMutexProfileFraction(5)
		runtime.SetBlockProfileRate(5)

		go func() {
			fmt.Println("pprof running on :6060")
			log.Println(http.ListenAndServe(":6060", nil))
		}()
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGTERM,
		syscall.SIGABRT,
		syscall.SIGKILL,
	)

	defer stop()

	shared.MustSetupInstrumenter(cfg)

	// Register video processor factory for FFmpeg operations with executor support
	log.Println("🎬 Registering video processor factory with executors...")
	ffmpeg.RegisterVideoProcessorFactoryWithExecutors("config/actions.yaml")
	log.Println("✅ Video processor factory registration complete (with executor support)")

	// db, err := sql.Open("postgres", cfg.PgURL.String())
	// if err != nil {
	// 	log.Fatalf("failed to connect to postgres")
	// 	return
	// }
	// defer db.Close()
	//
	// if err := db.Ping(); err != nil {
	// }

	// svc := mediametadata.NewMediaMetadaService(
	// mediametadata.NewMediaMetadataRepo("postgres", db))

	// Separate queues for images and videos
	imageQueue := make(chan *mediahose.Job, cfg.QSize)
	videoQueue := make(chan *mediahose.Job, cfg.QSize/4) // Smaller queue for videos
	batchQueue := make(chan *mediahose.BatchedJob, cfg.QSize)

	// Image worker scaler
	imageScaler := &mediahose.DynamicScaler[*mediahose.Job]{
		WorkerFactory: func(idx int64, done chan int64) mediahose.Worker[*mediahose.Job] {
			return &mediahose.FetchWorker{Idx: idx, CloserChan: done}
		},
		Queue:              imageQueue,
		MinWorkers:         2,
		MaxWorkers:         10,
		ScaleUpThreshold:   10,
		ScaleDownThreshold: 1,
		ScaleSigChan:       make(chan struct{}),
		Name:               "ImageWorker",
	}

	mediahose.BootStrapDynamicScalerFrom(imageScaler).Start(ctx)

	// Video worker scaler (different config for expensive FFmpeg operations)
	videoScaler := &mediahose.DynamicScaler[*mediahose.Job]{
		WorkerFactory: func(idx int64, done chan int64) mediahose.Worker[*mediahose.Job] {
			return &mediahose.FetchWorker{Idx: idx, CloserChan: done}
		},
		Queue:              videoQueue,
		MinWorkers:         1,
		MaxWorkers:         5,
		ScaleUpThreshold:   3,
		ScaleDownThreshold: 0,
		ScaleSigChan:       make(chan struct{}),
		Name:               "VideoWorker",
	}

	mediahose.BootStrapDynamicScalerFrom(videoScaler).Start(ctx)

	dispatcher := mediahose.NewDispatcher(100*time.Millisecond, batchQueue, mediahose.NoOpOnComplete)
	dispatcher.RunInBackground(ctx)

	profiler := shared.NewProfiler(":6060")
	profiler.Listen(ctx)

	http.HandleFunc("/optimux/ping", func(w http.ResponseWriter, r *http.Request) {
		err := shared.I().Incr("optimux.ping.200", shared.GetServerTags(), 1)
		if err != nil {
			log.Printf("failed to push metric to datadog. err %v\n", err)
		}

		log.Printf("Incoming Request: Origin: %s, Host: %s", r.Header.Get("Origin"), r.Host)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong"))
	})

	http.HandleFunc("/optimux/docs", handlers.DocsHandler(cfg.Quality))

	{
		// handler to push http2 header for a given batch of requests
		mdhandler := &handlers.MediaDistributionHandler{}

		http.HandleFunc("/optimux/preload", mdhandler.Handle(cfg))
	}

	http.HandleFunc("/admin/debug", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		query := r.URL.Query()

		state := query.Get("state")
		if state != "on" && state != "off" {
			http.Error(w, "need ?state=on/off", http.StatusUnprocessableEntity)
			return
		}

		var msg string

		if state == "on" {
			profiler.On(ctx)
			msg = "debug port is on"
		} else {
			profiler.Off(ctx)
			msg = "debug port is off"
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(msg))

		// return
	})

	{
		// Unified video handler (routes to sprite/transcoding based on format param)
		videoHandler := handlers.NewVideoAssetHandler(
			s3Client,
			cfg.DefaultS3Bucket,
			videoQueue,
			videoScaler,
			encoders.StreamEncoder,
		)

		assetsHandler := handlers.ChainMiddlewares(
			videoHandler.Handle(cfg, "/optimux/assets", cfg.Env),

			func(next http.HandlerFunc) http.HandlerFunc {
				return handlers.ValidateExpectedFormatWithEnv(next, cfg.Env)
			},
			func(next http.HandlerFunc) http.HandlerFunc {
				return handlers.ValidateAndSetMediaType(next, handlers.ExtractFromPath)
			},
			func(next http.HandlerFunc) http.HandlerFunc {
				return handlers.AddCorsHeaders(next, handlers.NewCorsConfig(cfg.Origins))
			},
		)

		http.HandleFunc("GET /optimux/assets/videos/", assetsHandler)
	}

	// Dynamic Single Asset Handler
	{
		// Image handler
		s3Handler := &handlers.S3ProxyImageHandler{
			JobQ:   imageQueue,
			Scaler: imageScaler,
			// MetadataSvc: svc,
		}

		assetsHandler := handlers.ChainMiddlewares(
			s3Handler.Handle(cfg.Quality, cfg, "/optimux/assets", cfg.Env),

			func(next http.HandlerFunc) http.HandlerFunc {
				return handlers.ValidateExpectedFormatWithEnv(next, cfg.Env)
			},
			func(next http.HandlerFunc) http.HandlerFunc {
				return handlers.ValidateAndSetMediaType(next, handlers.ExtractFromPath)
			},
			func(next http.HandlerFunc) http.HandlerFunc {
				return handlers.AddCorsHeaders(next, handlers.NewCorsConfig(cfg.Origins))
			},
		)
		http.HandleFunc("GET /optimux/assets/", assetsHandler)
	}

	{
		// batchedS3Handler := &handlers.BatchedMediaHandler{
		// 	Catergory:   "s3",
		// 	Dispatcher:  dispatcher,
		// 	Scaler:      batchScaler,
		// 	JobQ:        batchQueue,
		// 	MetadataSvc: svc,
		// }
		//
		// assetsHandler := handlers.ChainMiddlewares(
		// 	batchedS3Handler.Handle(cfg.Quality, cfg, "/optimux/bulk/assets", cfg.Env),
		//
		// 	func(next http.HandlerFunc) http.HandlerFunc {
		// 		return handlers.ValidateExpectedFormat(next)
		// 	},
		// 	func(next http.HandlerFunc) http.HandlerFunc {
		// 		return handlers.ValidateAndSetMediaType(next, handlers.ExtractFromPath)
		// 	},
		// 	func(next http.HandlerFunc) http.HandlerFunc {
		// 		return handlers.AddCorsHeaders(next, handlers.NewCorsConfig(cfg.Origins))
		// 	},
		// )
		//
		// http.HandleFunc("GET /optimux/bulk/assets/", assetsHandler)
	}

	// Static Assets Handler
	{
		staticHandler := &handlers.S3ProxyImageHandler{
			JobQ:   imageQueue,
			Scaler: imageScaler,
			// MetadataSvc: svc,
		}

		staticSssetsHandler := handlers.ChainMiddlewares(
			staticHandler.Handle(cfg.Quality, cfg, "/optimux/static", fmt.Sprintf("%s/assets", cfg.Env)),
			func(next http.HandlerFunc) http.HandlerFunc {
				return handlers.ValidateExpectedFormatWithEnv(next, cfg.Env)
			},
			func(next http.HandlerFunc) http.HandlerFunc {
				return handlers.ValidateAndSetMediaType(next, handlers.ExtractFromPath)
			},
			func(next http.HandlerFunc) http.HandlerFunc {
				return handlers.AddCorsHeaders(next, handlers.NewCorsConfig(cfg.Origins))
			},
		)
		http.HandleFunc("GET /optimux/static/", staticSssetsHandler)
	}

	{
		// conn, err := pgx.Connect(ctx, cfg.PgURL.String())
		// if err != nil {
		// 	log.Fatalf("failed to connect to postgres. %v", err)
		// 	return
		// }
		//
		// defer conn.Close(ctx)

		// dt, err := conn.LoadType(ctx, "media_version_pair")
		// if err != nil {
		// 	log.Fatal("failed to load type", err)
		// }

		// conn.TypeMap().RegisterType(dt)

		// Reverse Proxy handler
		// proxyHandle := &handlers.ProxyHandler{
		// 	Reg:         registry.DefaultSourceRegistry(cfg),
		// 	Enabled:     cfg.IsEnvLocal() || true,
		// 	MetadataSvc: svc,
		// }
		//
		// proxyHandler := handlers.ChainMiddlewares(
		// 	proxyHandle.Handle(cfg.Quality, cfg, "/optimux/media", true),
		// 	func(next http.HandlerFunc) http.HandlerFunc {
		// 		return handlers.AddCorsHeaders(
		// 			next,
		// 			handlers.NewCorsConfig(cfg.Origins),
		// 		)
		// 	},
		// )
		//
		// http.HandleFunc("GET /optimux/media/", proxyHandler)
	}

	server := &http.Server{
		Addr:    cfg.Port,
		Handler: http.DefaultServeMux,
	}

	if !cfg.UseHttp2 {
		fmt.Println("Server running on", cfg.Port)
		log.Fatal(http.ListenAndServe(cfg.Port, nil))
		return
	}

	cert, err := tls.LoadX509KeyPair("server.crt", "server.key")
	if err != nil {
		log.Fatal("Failed to load key pair:", err)
	}

	tlsConfig := &tls.Config{
		Certificates:             []tls.Certificate{cert},
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP256, tls.X25519},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}
	server.TLSConfig = tlsConfig

	http2.ConfigureServer(server, &http2.Server{
		MaxConcurrentStreams: 250,
	})

	fmt.Println("TLS Server running on", cfg.Port)

	log.Fatal(server.ListenAndServeTLS("server.crt", "server.key"))
	return
}
