package generations

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-batteries/slicendice"
	"github.com/jackc/pgx/v5"
)

type GenerationRepo struct {
	DB *pgx.Conn
}

type FetchParams struct {
	OldestOffset time.Time
	CursorOffset time.Time
	Limit        int32
	UserID       int
	ExitAtOnce   bool
	OnlyCount    bool
}

func (self *GenerationRepo) CountImagesForStudio(ctx context.Context, qparams *FetchParams) (int64, error) {
	q := New(self.DB)

	params := CountSuccessfullImageGenerationParams{
		Type:        "studio",
		CreatedAt:   qparams.CursorOffset,
		CreatedAt_2: qparams.OldestOffset,
	}

	log.Println(countSuccessfullImageGeneration, params)

	return q.CountSuccessfullImageGeneration(ctx, params)
}

func (self *GenerationRepo) CountImagesForStudioByUser(ctx context.Context, qparams *FetchParams) (int64, error) {
	q := New(self.DB)

	params := CountSuccessfullImageGenerationByUserParams{
		Type:        "studio",
		CreatedAt:   qparams.CursorOffset,
		CreatedAt_2: qparams.OldestOffset,
		UserID:      int32(qparams.UserID),
	}

	log.Println(countSuccessfullImageGenerationByUser, params)

	return q.CountSuccessfullImageGenerationByUser(ctx, params)
}

func (self *GenerationRepo) FetchImagesForStudio(ctx context.Context, qparams *FetchParams) <-chan []*Generation {
	q := New(self.DB)

	resultCh := make(chan []*Generation, 1)
	lastInterval := qparams.CursorOffset

	go func() {
		hasMore := true
		defer close(resultCh)

		for hasMore {
			params := ListSuccessfullImageGenerationParams{
				Type:        "studio",
				CreatedAt:   lastInterval,
				CreatedAt_2: qparams.OldestOffset,
				Limit:       qparams.Limit + 1,
			}

			fmt.Printf("search params %v\n", params)

			select {
			case <-ctx.Done():
				log.Println("exiting due to signal")
				return
			default:
			}

			rows, err := q.ListSuccessfullImageGeneration(ctx, params)
			if err != nil {
				log.Println("failed to get any records", err)
				return
			}

			nRows := len(rows)

			hasMore = nRows > int(qparams.Limit)
			if nRows > 1 {
				rows = rows[:nRows-1]
				nRows = nRows - 1
			}

			fmt.Println("has more", hasMore, nRows)

			if nRows == 0 {
				fmt.Println("no more data")
				return
			}

			generations := slicendice.Map(rows, func(
				row ListSuccessfullImageGenerationRow, _ int,
			) *Generation {
				return &Generation{
					GenerationID:    row.GenerationID,
					MediaID:         row.MediaID,
					OutputMediaPath: row.OutputMediaPath,
					CreatedAt:       row.CreatedAt,
				}
			})

			fmt.Printf("records fetched %d ", len(generations))

			lastInterval = generations[len(generations)-1].CreatedAt

			fmt.Println("next interval", lastInterval)

			resultCh <- generations

			if qparams.ExitAtOnce {
				log.Println("forcefully exiting")
				return
			}

			fmt.Println("records send for processing")
		}
	}()

	return resultCh
}

func (self *GenerationRepo) FetchImagesForStudioByUser(ctx context.Context, qparams *FetchParams) <-chan []*Generation {
	q := New(self.DB)

	resultCh := make(chan []*Generation, 1)
	lastInterval := qparams.CursorOffset

	go func() {
		hasMore := true
		defer close(resultCh)

		if qparams.UserID == 0 {
			fmt.Println("userid is missing. exit")
			return
		}

		for hasMore {
			params := ListSuccessfullImageGenerationByUserParams{
				Type:        "studio",
				CreatedAt:   lastInterval,
				CreatedAt_2: qparams.OldestOffset,
				Limit:       qparams.Limit + 1,
				UserID:      int32(qparams.UserID),
			}

			fmt.Printf("search params %+v\n", params)

			select {
			case <-ctx.Done():
				log.Println("exiting due to signal")
				return
			default:
			}

			rows, err := q.ListSuccessfullImageGenerationByUser(ctx, params)
			if err != nil {
				log.Println("failed to get any records", err)
				return
			}

			nRows := len(rows)

			hasMore = nRows > int(qparams.Limit)
			if nRows > 1 {
				rows = rows[:nRows-1]
				nRows = nRows - 1
			}

			fmt.Println("has more", hasMore, nRows)

			if nRows == 0 {
				fmt.Println("no more data")
				return
			}

			generations := slicendice.Map(rows, func(
				row ListSuccessfullImageGenerationByUserRow, _ int,
			) *Generation {
				return &Generation{
					GenerationID:    row.GenerationID,
					MediaID:         row.MediaID,
					OutputMediaPath: row.OutputMediaPath,
					CreatedAt:       row.CreatedAt,
					UserID:          int32(qparams.UserID),
				}
			})

			fmt.Printf("records fetched %d ", len(generations))
			lastInterval = generations[len(generations)-1].CreatedAt

			fmt.Println("next interval", lastInterval)

			resultCh <- generations

			if qparams.ExitAtOnce {
				log.Println("forcefully exiting")
				return
			}

			fmt.Println("records send for processing")
		}
	}()

	return resultCh
}
