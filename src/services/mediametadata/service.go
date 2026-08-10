package mediametadata

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/go-batteries/slicendice"
	"github.com/go-batteries/optimux/src/shared"
)

type MetadataService interface {
	BatchAndCreateMediaMetadata(ctx context.Context, mediaDatas ...*MediaMetadata) (int, error)
	FetchPreComputedFromRequest(ctx context.Context, mediaRequests ...*DistributionRequest) ([]*MediaMetadata, error)
	FetchPreComputedFromMetadata(ctx context.Context, requests ...*MediaMetadata) ([]*MediaMetadata, error)
}

type MediaMetadataService struct {
	Repo Repository
}

func NewMediaMetadaService(repo Repository) *MediaMetadataService {
	return &MediaMetadataService{
		Repo: repo,
	}
}

func (svc *MediaMetadataService) BatchAndCreateMediaMetadata(ctx context.Context, mediaDatas ...*MediaMetadata) (int, error) {
	combinedSizesData := []*MediaMetadata{}

	mediaIDMap := map[string]*MediaMetadata{}

	for _, data := range mediaDatas {
		key := data.Index()
		exists, ok := mediaIDMap[key]
		if !ok {
			mediaIDMap[key] = data
			continue
		}

		exists.MetadataSchema.Sizes = append(exists.MetadataSchema.Sizes, data.MetadataSchema.Sizes...)
		mediaIDMap[key] = exists
	}

	for _, metadata := range mediaIDMap {
		if metadata.MetadataSchema == nil {
			continue
		}

		b, err := json.Marshal(metadata.MetadataSchema)
		if err != nil {
			log.Printf("failed to marshal metadata schema. error %v", err)
			continue
		}

		metadata.Metadata = b
		combinedSizesData = append(combinedSizesData, metadata)
	}

	// log.Println(shared.Dumps(combinedSizesData))

	err := svc.Repo.Create(ctx, combinedSizesData)
	if err != nil {
		log.Printf("failed to create media metadata. error %v", err)
	}

	return len(combinedSizesData), err
}

// Converts file names begining with img_
func BuildMediaMetadataFromReq(requests ...*DistributionRequest) []*MediaMetadata {
	reducer := func(req *DistributionRequest, _ int) *MediaMetadata {
		filePath := req.MediaPath

		// _, err := url.Parse(filePath)
		// if err != nil {
		// 	return nil
		// }

		_, _, ok := shared.ExplodeFileName(filePath)
		if !ok {
			return nil
		}

		return BuildV1MediaMetadataWithDefaults(filePath)
	}

	return slicendice.Reduce(
		requests,
		func(acc []*MediaMetadata, r *DistributionRequest, i int) []*MediaMetadata {
			t := reducer(r, i)
			if t != nil {
				acc = append(acc, t)
			}

			return acc
		},
		[]*MediaMetadata{},
	)
}

func (svc *MediaMetadataService) FetchPreComputedFromRequest(ctx context.Context,
	requests ...*DistributionRequest,
) ([]*MediaMetadata, error) {
	log.Println("fetching processed media metadata from file")

	sanitizedRequests := BuildMediaMetadataFromReq(requests...)

	// log.Println("sanitized reqyests", shared.Dumps(sanitizedRequests))

	fetchedData, hasError, err := svc.Repo.SelectMedias(ctx, sanitizedRequests)
	if err != nil {
		return nil, err
	}

	log.Println("fetched data result", len(fetchedData))

	if hasError {
		log.Println("partial data is fetched")
	}

	return FilterResults(ctx, sanitizedRequests, fetchedData), nil
}

func (svc *MediaMetadataService) FetchPreComputedFromMetadata(ctx context.Context,
	requests ...*MediaMetadata,
) ([]*MediaMetadata, error) {
	log.Println("fetching processed media metadata from file")

	fetchedData, hasError, err := svc.Repo.SelectMedias(ctx, requests)
	if err != nil {
		return nil, err
	}

	log.Println("fetched data result", len(fetchedData))
	fmt.Println(shared.Dumps(fetchedData))

	if hasError {
		log.Println("partial data is fetched")
	}

	return FilterResults(ctx, requests, fetchedData), nil
}

func FilterResults(ctx context.Context, requestedData []*MediaMetadata, fetchedData []*MediaMetadata) []*MediaMetadata {
	reqMap := make(map[string]map[string]struct{})

	for _, req := range requestedData {
		key := req.Index()

		if req.MetadataSchema == nil {
			continue
		}

		for _, sz := range req.MetadataSchema.Sizes {
			if sz == nil {
				continue
			}

			if _, ok := reqMap[key]; !ok {
				reqMap[key] = make(map[string]struct{})
			}

			reqMap[key][sz.Key()] = struct{}{}
		}
	}

	// log.Println("=================================")
	// log.Println(shared.Dumps(requestedData))
	//
	// log.Println("=================================")
	// log.Println(shared.Dumps(fetchedData))

	// Filter enriched data
	filteredResults := make([]*MediaMetadata, 0, len(fetchedData))

	for _, data := range fetchedData {
		if err := data.Enrich(); err != nil {
			log.Println("data enrichment failed for media ID", data.MediaID, err)
			// log.Fatal(err)
			continue
		}

		key := data.Index()
		expectedSizes, ok := reqMap[key]
		if !ok {
			log.Println("image not post processed", key)
			continue
		}

		// hasMatch := false
		hasMatch := slicendice.Some(data.MetadataSchema.Sizes, func(sz *SizeFormatTuple, _ int) bool {
			if sz == nil {
				return false
			}

			_, ok := expectedSizes[sz.Key()]
			return ok
		})

		// for _, sz := range data.MetadataSchema.Sizes {
		// 	if sz == nil {
		// 		continue
		// 	}
		// 	if _, ok := expectedSizes[sz.Key()]; ok {
		// 		hasMatch = true
		// 		break
		// 	}
		// }

		log.Println("post process has matching size", hasMatch)

		// log.Println("has match", hasMatch, shared.Dumps(data.MetadataSchema.Sizes), expectedSizes)

		if hasMatch {
			filteredResults = append(filteredResults, data)
		}
	}

	return filteredResults
}
