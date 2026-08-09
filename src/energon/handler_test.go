package energon

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// MockWorker mimics BatchWorker but does not perform actual image/video processing.
type MockWorker struct {
	Idx        int64
	CloserChan chan int64
}

//
// // Work processes jobs in the queue but mocks actual media processing.
// func (mw *MockWorker) Work(ctx context.Context, jobQueueChan <-chan *mediahose.BatchedJob) {
// 	defer func() {
// 		mw.CloserChan <- mw.Idx // Signal shutdown
// 	}()
//
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			log.Println("[MockWorker] Exiting...")
// 			return
// 		case batch := <-jobQueueChan:
// 			log.Printf("[MockWorker] Processing batch UID: %s, %d jobs\n", batch.UID, len(batch.Jobs))
//
// 			// Simulating processing without downloading/uploading
// 			for _, job := range batch.Jobs {
// 				log.Printf("[MockWorker] Pretending to process media: %s -> %s\n", job.MediaType, job.ImagePath)
//
// 				// Simulating processing delay
// 				<-ctx.Done()
//
// 				// Simulate media encoding
// 				key := fmt.Sprintf("%d_%d_%d.%s", job.Sizes[0][0], job.Sizes[0][1], job.Quality, job.Format)
//
// 				log.Println(key)
//
// 				// Instead of encoding/uploading, just log it
// 				log.Printf("[MockWorker] Simulating encoding and upload for: %s (bucket: %s, key: %s)\n",
// 					job.ImagePath, *job.S3Bucket, *job.S3Key)
//
// 				// Mark job as done
// 				close(job.Done)
// 			}
// 		}
// 	}
// }
//
// // Test ImageProcessorHandler with MockWorker and MockS3Client
// func TestHandle_ValidImage_MockWorker(t *testing.T) {
// 	mockWorker := &MockWorker{
// 		Idx:        time.Now().Unix(),
// 		CloserChan: make(chan int64),
// 	}
//
// 	mockS3Client := &MockS3Client{}
//
// 	handler := &ImageProcessorHandler{
// 		S3Client:   nil, // Not needed since we're mocking the uploader
// 		Worker:     mockWorker,
// 		BatchQueue: make(chan *mediahose.BatchedJob, 10),
// 		Encoder:    mockS3Client.Upload,
// 	}
//
// 	record := &LambdaRecord{
// 		Bucket:       "test-bucket",
// 		UploadedPath: "user/img_1234.webp",
// 		Region:       "us-east-1",
// 		BatchID:      "batch123",
// 	}
//
// 	dones, err := handler.Handle(context.Background(), record)
//
// 	assert.NoError(t, err)
// 	assert.NotNil(t, dones)
// 	assert.True(t, len(dones) > 0)
//
// 	// Ensure jobs complete
// 	for _, done := range dones {
// 		<-done
// 	}
// }
//
// // Test for invalid file format
// func TestHandle_InvalidMedia_MockWorker(t *testing.T) {
// 	mockWorker := &MockWorker{
// 		Idx:        time.Now().Unix(),
// 		CloserChan: make(chan int64),
// 	}
//
// 	handler := &ImageProcessorHandler{
// 		Worker:     mockWorker,
// 		BatchQueue: make(chan *mediahose.BatchedJob, 10),
// 	}
//
// 	record := &LambdaRecord{
// 		Bucket:       "test-bucket",
// 		UploadedPath: "user/randomfile.txt",
// 		Region:       "us-east-1",
// 		BatchID:      "batch123",
// 	}
//
// 	dones, err := handler.Handle(context.Background(), record)
//
// 	assert.Error(t, err)
// 	assert.Nil(t, dones)
// }
//
// // Test S3 Upload Failure
// func TestHandle_S3UploadError_MockWorker(t *testing.T) {
// 	mockWorker := &MockWorker{
// 		Idx:        time.Now().Unix(),
// 		CloserChan: make(chan int64),
// 	}
//
// 	mockS3Client := &MockS3Client{}
//
// 	handler := &ImageProcessorHandler{
// 		S3Client:   nil,
// 		Worker:     mockWorker,
// 		BatchQueue: make(chan *mediahose.BatchedJob, 10),
// 		Encoder:    mockS3Client.Upload,
// 	}
//
// 	record := &LambdaRecord{
// 		Bucket:       "test-bucket",
// 		UploadedPath: "error_case",
// 		Region:       "us-east-1",
// 		BatchID:      "batch123",
// 	}
//
// 	dones, err := handler.Handle(context.Background(), record)
//
// 	assert.Error(t, err)
// 	assert.Nil(t, dones)
// }

func Test_generateObjectOathForSize(t *testing.T) {
	t.Run("should append the sizeId to the filename keeping the extension", func(t *testing.T) {
		uploadPath := "user_generated/usr_ezjjIL2JUq/img_0ZsJnB5E07.webp"
		sizeId := "@2x"

		result := generateObjectPathForSize(uploadPath, sizeId)

		require.Equal(t, "img_0ZsJnB5E07_@2x.webp", result)
	})
}
