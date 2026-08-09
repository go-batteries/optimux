package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/jackc/pgx/v5"
	"github.com/roverxio/optimux/src/config"
	"github.com/roverxio/optimux/src/mediahose"
	"github.com/roverxio/optimux/src/services/generations"
	"github.com/roverxio/optimux/src/services/mediametadata"
	"github.com/roverxio/optimux/src/shared"

	_ "net/http/pprof"
)

var (
	rpgURL       string
	pgURL        string
	region       string
	s3Bucket     string
	vipsThreads  int
	oldestOffset int
	uptoOffset   int
	limit        int
	userID       int
	vthreads     int
	onlyCount    bool
	runOnce      bool
	doProfile    bool
)

func main() {
	ctx := context.Background()

	flag.StringVar(&rpgURL, "rpgurl", "", "postgres read url postgres://user:pass@host:port/db_name?sslmode=enabled")
	flag.StringVar(&pgURL, "pgurl", "", "postgres url postgres://user:pass@host:port/db_name?sslmode=enabled")
	flag.StringVar(&region, "region", "us-east-1", "aws region")
	flag.StringVar(&s3Bucket, "bucket", "", "default aws bucket")

	flag.IntVar(&userID, "userid", 0, "if populate by user id")
	flag.IntVar(&vipsThreads, "vthreads", 1000, "vips threads to spin off")
	flag.IntVar(&oldestOffset, "since", 92, " number of days to go back to.")
	flag.IntVar(&uptoOffset, "to", 1, "number of days to track the newest offset back n forth")
	flag.IntVar(&limit, "limit", 500, " page size.")

	flag.BoolVar(&doProfile, "pprof", false, "enable profiling apis")
	flag.BoolVar(&onlyCount, "count", false, "get only count of records to migrate")
	flag.BoolVar(&runOnce, "once", false, "run only once")

	flag.Parse()

	if rpgURL == "" || pgURL == "" {
		log.Fatal("pg url missing")
	}

	if oldestOffset == 0 {
		log.Fatal("oldest offset can't be 0")
	}

	if region == "" {
		region = os.Getenv("AWS_REGION")
	}

	if region == "" {
		log.Fatal("aws region needed")
	}

	if s3Bucket == "" {
		log.Fatal("default s3 bucket not provided")
	}

	var logWriter io.Writer = os.Stderr

	log.SetOutput(logWriter)
	log.SetFlags(log.Lshortfile | log.Lmicroseconds)

	if doProfile {
		runtime.SetMutexProfileFraction(5)
		runtime.SetBlockProfileRate(5)

		go func() {
			fmt.Println("pprof running on :6060")
			log.Println(http.ListenAndServe(":6060", nil))
		}()
	}

	batchQueue := make(chan *mediahose.BatchedJob, 1000)
	defer close(batchQueue)

	log.Println("connecting to db")

	conn, err := pgx.Connect(ctx, rpgURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}

	if err := conn.Ping(ctx); err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}

	defer conn.Close(ctx)

	db, err := sql.Open("postgres", pgURL)
	if err != nil {
		log.Fatalf("failed to connect to postgres %v", err)
	}

	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to connect to db. error %v", err)
	}

	log.Println("connected to databases")

	shared.MustSetupInstrumenter(&config.Config{StatsDAddr: ""})

	metasvc := mediametadata.NewMediaMetadaService(
		mediametadata.NewMediaMetadataRepo("postgres", db))

	vips.Startup(&vips.Config{ConcurrencyLevel: int(vipsThreads)})
	defer vips.Shutdown()

	svc := generations.NewStudioImageBatchService(
		region,
		s3Bucket,
		&generations.GenerationRepo{DB: conn},
		metasvc,
		batchQueue,
	)

	now := time.Now().UTC()
	old := now.AddDate(0, 0, -oldestOffset)

	if uptoOffset > 0 {
		now = now.AddDate(0, 0, -uptoOffset)
	}

	err = svc.ProcessStudioImages(ctx, &generations.FetchParams{
		OldestOffset: old,
		CursorOffset: now,
		Limit:        int32(limit),
		UserID:       userID,
		OnlyCount:    onlyCount,
		ExitAtOnce:   runOnce,
	})
	if err != nil {
		fmt.Println("failed to process records")
	}
}
