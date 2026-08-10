package generations

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-batteries/optimux/src/encoders"
	"github.com/go-batteries/optimux/src/mediahose"
	"github.com/go-batteries/optimux/src/services/mediametadata"
	"github.com/go-batteries/optimux/src/shared"

	"github.com/go-batteries/optimux/src/energon"
)

type SaveRequest struct {
	Data []*mediametadata.MediaMetadata
	Done chan error
}

type StudioImageBatchService struct {
	Repo       *GenerationRepo
	S3Client   *s3.Client
	S3Bucket   string
	Worker     *mediahose.BatchWorker
	Dispatcher *mediahose.Dispatcher
	Metadata   *mediametadata.MediaMetadataService
	Encoders   *encoders.S3Uploader
	Queue      chan *mediahose.BatchedJob
	SaveQueue  chan SaveRequest
}

func NewStudioImageBatchService(
	region string,
	bucket string,
	repo *GenerationRepo,
	metadataSvc *mediametadata.MediaMetadataService,
	queue chan *mediahose.BatchedJob,
) *StudioImageBatchService {
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		log.Fatal("failed to initialize config")
	}

	s3Client := s3.NewFromConfig(cfg)

	worker := &mediahose.BatchWorker{
		Idx:         time.Now().UTC().Unix(),
		CloserChan:  make(chan int64),
		ConsumeResp: true,
	}

	dispatcher := mediahose.NewDispatcher(100*time.Millisecond, queue, mediahose.NoOpOnComplete)
	dispatcher.Events = mediahose.NewBatchedEventEmitter()

	encoder := encoders.NewS3Uploader(s3Client)

	svc := &StudioImageBatchService{
		Repo:       repo,
		S3Client:   s3Client,
		Worker:     worker,
		Dispatcher: dispatcher,
		Encoders:   encoder,
		Queue:      queue,
		Metadata:   metadataSvc,
		SaveQueue:  make(chan SaveRequest),
		S3Bucket:   bucket,
	}

	return svc
}

func (svc *StudioImageBatchService) ProcessStudioImages(ctx context.Context, params *FetchParams) error {
	imageProcessor := &energon.ImageProcessorHandler{
		S3Client:   svc.S3Client,
		Worker:     svc.Worker,
		BatchQueue: svc.Queue,
		Encoder:    svc.Encoders.Upload,
		Dispatcher: svc.Dispatcher,
	}

	var (
		totalRecords int64
		err          error
		imageCh      <-chan []*Generation
	)

	// ctx, cancel = context.WithCancel(ctx)

	if params.UserID == 0 {
		fmt.Println("fetching by date range")
		totalRecords, err = svc.Repo.CountImagesForStudio(ctx, params)
		if err != nil {
			log.Println("failed to count total records", *params)
			return err
		}

	} else {
		fmt.Println("fetching for user", params.UserID)

		totalRecords, err = svc.Repo.CountImagesForStudioByUser(ctx, params)
		if err != nil {
			log.Println("failed to count total records", *params)
			return err
		}
	}

	fmt.Println("total media to process", totalRecords,
		int(totalRecords)*len(shared.SizesForCompression))

	if params.OnlyCount {
		return nil
	}

	if params.UserID == 0 {
		imageCh = svc.Repo.FetchImagesForStudio(ctx, params)
	} else {
		imageCh = svc.Repo.FetchImagesForStudioByUser(ctx, params)
	}

	svc.Dispatcher.RunInBackground(ctx)
	go svc.Worker.Work(ctx, svc.Queue)

	go svc.Snapshot(ctx)
	defer close(svc.SaveQueue)

	semaphore := make(chan struct{}, 2) // concurrency 2
	var wg sync.WaitGroup

	idx := 0

	for generations := range imageCh {
		semaphore <- struct{}{}
		wg.Add(1)

		go func(i int) {
			defer wg.Done()
			defer func() { <-semaphore }()
			doneCh := make(chan struct{})

			svc.ProcessEach(ctx, imageProcessor, doneCh, generations)
			<-doneCh
		}(idx)

		idx += 1
	}

	wg.Wait()

	// fmt.Println("saved", totalCount)

	// createdCount, err := svc.Metadata.BatchAndCreateMediaMetadata(ctx, allMedia...)
	// if err != nil {
	// 	fmt.Println("failed to save to metadata record", err)
	// 	return err
	// }
	//
	// fmt.Printf("stored %d metadata entries", createdCount)

	log.Println("done processing")
	return nil
}

func (svc *StudioImageBatchService) ProcessEach(ctx context.Context,
	processor *energon.ImageProcessorHandler, done chan struct{}, generations []*Generation,
) (count int) {
	defer close(done)
	// defer func() {
	// 	fmt.Println("exiting", shared.Dumps(generations))
	// }()

	count = 0
	allMedia := []*mediametadata.MediaMetadata{}
	dones := []shared.MailBoxCh{}

	cancelFuncs := []context.CancelFunc{}

	defer func() {
		for _, cancel := range cancelFuncs {
			cancel()
		}
	}()

	for _, gen := range generations {
		if gen.OutputMediaPath == nil {
			log.Println("no output media path")
			continue
		}

		bucket, key, ok := shared.ExtractBucketAndKeyFromS3(*gen.OutputMediaPath)
		if !ok {
			log.Println("failed to get bucket or key for ", *gen.OutputMediaPath)
			continue
		}

		log.Println("processing", *gen.OutputMediaPath, "🪣 bucket", bucket, "key", key)

		cx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		cancelFuncs = append(cancelFuncs, cancel)

		record := &energon.LambdaRecord{
			Bucket:          bucket,
			UploadedPath:    key,
			Region:          "us-east-1",
			BatchID:         gen.GenerationID,
			Ctx:             cx,
			Cancel:          cancel,
			RequestedFormat: ".webp",
		}

		_dones, err := processor.Handle(ctx, record)
		if err != nil {
			fmt.Printf("record failed: %v\n", err)
			continue
		}

		dones = append(dones, _dones...)
	}

	fmt.Println("waiting to done", len(dones), "of", len(generations), "generations")

	if len(dones) == 0 {
		fmt.Println("nothing to process")
		return
	}

	now := time.Now()
	for _, done := range dones {
		res := <-done

		log.Println("received", res, "on", done)

		if meta, ok := res.(*mediametadata.MediaMetadata); ok {
			// fmt.Println("received from ch", shared.Dumps(meta.MetadataSchema.Sizes))
			allMedia = append(allMedia, meta)
		} else {
			fmt.Println(now, "fail", done, res)
		}

		// if res, ok := <-done; ok {
		// 	if meta, ok := res.(*mediametadata.MediaMetadata); ok {
		// 		allMedia = append(allMedia, meta)
		// 	} else {
		// 		fmt.Println("fail")
		// 	}
		// } else {
		// 	fmt.Println("waiting expired")
		// }
	}

	count = len(allMedia)

	if count == 0 {
		log.Println("nothing to save")
		return
	}

	data := mediametadata.JoinSchema(allMedia)

	fmt.Println("migrated", shared.Dumps(data))

	waitChan := make(chan error)
	svc.SaveQueue <- SaveRequest{Data: data, Done: waitChan}
	<-waitChan

	return count
}

func (svc *StudioImageBatchService) Snapshot(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-svc.SaveQueue:
			fmt.Println("saving")
			createdCount, err := svc.Metadata.BatchAndCreateMediaMetadata(ctx, req.Data...)
			if err != nil {
				log.Println("failed to save to metadata record")
				fmt.Println("failed to save to metadata record", shared.Dumps(req.Data))
			} else {
				fmt.Println("created records", createdCount)
			}

			mediametadata.UpdateS3Headers(ctx, svc.S3Bucket, svc.S3Client, req.Data...)
			req.Done <- err
		}
	}
}
