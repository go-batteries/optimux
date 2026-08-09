package mediametadata

import (
	"context"
	"errors"
	"log"
	"reflect"
	"sort"
	"testing"

	"github.com/roverxio/optimux/src/shared"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func sortMediaMetadata(items []*MediaMetadata) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].MediaID == items[j].MediaID {
			return items[i].Version < items[j].Version
		}
		return items[i].MediaID < items[j].MediaID
	})
}

func Test_BatchAndCreateMedia(t *testing.T) {
	mediaMetadatas := []*MediaMetadata{
		{
			MediaID: "img_1234.webp",
			Source:  "s3",
			Version: "v1",
			MetadataSchema: &V1MedatataSchema{
				Sizes: []*SizeFormatTuple{{Size: "@2x", Format: ".webp"}},
			},
		},
		{
			MediaID: "img_1234.webp",
			Source:  "s3",
			Version: "v1",
			MetadataSchema: &V1MedatataSchema{
				Sizes: []*SizeFormatTuple{{Size: "@3x", Format: ".webp"}},
			},
		},
		{
			MediaID: "img_1234.webp",
			Source:  "s3",
			Version: "v2",
			MetadataSchema: &V1MedatataSchema{
				Sizes: []*SizeFormatTuple{{Size: "@2x", Format: ".webp"}},
			},
		},
		{
			MediaID: "img_4678.webp",
			Source:  "s3",
			Version: "v1",
			MetadataSchema: &V1MedatataSchema{
				Sizes: []*SizeFormatTuple{{Size: "@2x", Format: ".webp"}},
			},
		},
	}

	t.Run("returns error if Create fails", func(t *testing.T) {
		mockRepo := new(MockMediaMetadataRepo)

		ctx := context.Background()
		svc := NewMediaMetadaService(mockRepo)

		mockRepo.On("Create", ctx, mock.Anything).Return(errors.New("failed to create"))

		_, err := svc.BatchAndCreateMediaMetadata(ctx, mediaMetadatas...)
		require.Error(t, err)
	})

	t.Run("should group the available sizes into a single metadata record", func(t *testing.T) {
		expectedMetadatas := []*MediaMetadata{
			{
				MediaID: "img_1234.webp",
				Source:  "s3",
				Version: "v1",
				MetadataSchema: &V1MedatataSchema{
					Sizes: []*SizeFormatTuple{
						{Size: "@2x", Format: ".webp"},
						{Size: "@3x", Format: ".webp"},
					},
				},
			},
			{
				MediaID: "img_1234.webp",
				Source:  "s3",
				Version: "v2",
				MetadataSchema: &V1MedatataSchema{
					Sizes: []*SizeFormatTuple{{Size: "@2x", Format: ".webp"}},
				},
			},
			{
				MediaID: "img_4678.webp",
				Source:  "s3",
				Version: "v1",
				MetadataSchema: &V1MedatataSchema{
					Sizes: []*SizeFormatTuple{{Size: "@2x", Format: ".webp"}},
				},
			},
		}

		mockRepo := new(MockMediaMetadataRepo)

		ctx := context.Background()
		svc := NewMediaMetadaService(mockRepo)

		// log.Printf("%+v", expectedMetadatas)

		// mockRepo.On("Create", ctx, expectedMetadatas).Return(nil)

		mockRepo.On("Create", ctx, mock.MatchedBy(func(inputs []*MediaMetadata) bool {
			sortMediaMetadata(expectedMetadatas)
			sortMediaMetadata(inputs)

			log.Println("expected", shared.Dumps(expectedMetadatas))
			log.Println("got", shared.Dumps(inputs))

			for i, expected := range expectedMetadatas {
				if expected.MediaID != inputs[i].MediaID {
					return false
				}

				if expected.Version != inputs[i].Version {
					return false
				}

				if reflect.DeepEqual(expected.Metadata, inputs[i].Metadata) {
					return false
				}
			}

			return true
		})).Return(nil)

		_, err := svc.BatchAndCreateMediaMetadata(ctx, mediaMetadatas...)
		require.NoError(t, err)

		mockRepo.AssertExpectations(t)
	})
}
