package mediahose

import (
	"context"
	"fmt"
	"log"
)

// EnhancedBatchWorker processes both image and video jobs using JobProcessor interface
type EnhancedBatchWorker struct {
	Idx         int64
	CloserChan  chan int64
	ConsumeResp bool
	Factory     ProcessorFactory
	TempDir     string
}

func NewEnhancedBatchWorker(idx int64, closerChan chan int64, consumeResp bool, factory ProcessorFactory, tempDir string) *EnhancedBatchWorker {
	return &EnhancedBatchWorker{
		Idx:         idx,
		CloserChan:  closerChan,
		ConsumeResp: consumeResp,
		Factory:     factory,
		TempDir:     tempDir,
	}
}

func (ebw *EnhancedBatchWorker) Work(ctx context.Context, jobQueueChan <-chan *BatchedJob) {
	defer func() {
		ebw.CloserChan <- ebw.Idx
	}()

	for {
		select {
		case <-ctx.Done():
			log.Printf("EnhancedBatchWorker %d exiting...", ebw.Idx)
			return
		case batch := <-jobQueueChan:
			log.Printf("EnhancedBatchWorker %d processing batch: %s with %d jobs", ebw.Idx, batch.UID, len(batch.Jobs))

			for _, job := range batch.Jobs {
				// Get processor config from job metadata
				config := job.GetProcessorConfig()
				
				// Create appropriate processor based on media type and operation
				processor := ebw.Factory.CreateProcessor(job.MediaType, config.Operation, ebw.TempDir)
				if processor == nil {
					log.Printf("No processor available for media type %d, operation %s", job.MediaType, config.Operation)
					if ebw.ConsumeResp {
						ebw.sendFailureResponse(job, fmt.Sprintf("No processor for media type %d", job.MediaType))
					}
					close(job.Done)
					continue
				}

				// Set the processor in the job
				job.SetProcessor(processor)

				// Process the job
				if err := processor.Process(ctx, job); err != nil {
					log.Printf("Enhanced batch processing failed for job %s: %v", job.ID, err)
					if ebw.ConsumeResp {
						ebw.sendFailureResponse(job, err.Error())
					}
					close(job.Done)
					continue
				}

				if ebw.ConsumeResp {
					ebw.sendSuccessResponse(job)
				}

				log.Printf("EnhancedBatchWorker %d completed job: %s", ebw.Idx, job.ID)
				close(job.Done)
			}
		}
	}
}

func (ebw *EnhancedBatchWorker) sendFailureResponse(job *Job, errorMsg string) {
	response := map[string]interface{}{
		"job_id":    job.ID,
		"status":    "failed",
		"error":     errorMsg,
		"media_type": job.MediaType,
	}
	
	select {
	case job.MailBox <- response:
		log.Printf("✅ sent failure response for job %s", job.ID)
	default:
		log.Printf("⚠️ failure response send skipped - no receiver for job %s", job.ID)
	}
}

func (ebw *EnhancedBatchWorker) sendSuccessResponse(job *Job) {
	response := map[string]interface{}{
		"job_id":     job.ID,
		"status":     "completed",
		"media_type": job.MediaType,
		"operation":  job.GetProcessorConfig().Operation,
	}
	
	select {
	case job.MailBox <- response:
		log.Printf("✅ sent success response for job %s", job.ID)
	default:
		log.Printf("⚠️ success response send skipped - no receiver for job %s", job.ID)
	}
}

// EnhancedWorkerFactory creates enhanced workers for the dynamic scaler
func EnhancedWorkerFactory(factory ProcessorFactory, tempDir string, consumeResp bool) func(idx int64, done chan int64) Worker[*BatchedJob] {
	return func(idx int64, done chan int64) Worker[*BatchedJob] {
		return NewEnhancedBatchWorker(idx, done, consumeResp, factory, tempDir)
	}
}
