package mediametadata

import (
	"context"
	"testing"

	"github.com/go-batteries/slicendice"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockMediaMetadataRepo struct {
	mock.Mock
}

func (m *MockMediaMetadataRepo) Create(ctx context.Context, data []*MediaMetadata) error {
	args := m.Called(ctx, data)
	return args.Error(0)
}

func (m *MockMediaMetadataRepo) UpdateSizes(ctx context.Context, data *MediaMetadata) error {
	args := m.Called(ctx, data)
	return args.Error(0)
}

func (m *MockMediaMetadataRepo) SelectMedias(ctx context.Context, mediaQueries []*MediaMetadata) ([]*MediaMetadata, bool, error) {
	args := m.Called(ctx, mediaQueries)
	return args.Get(0).([]*MediaMetadata), args.Bool(1), args.Error(2)
}

func (m *MockMediaMetadataRepo) SelectMedias3(ctx context.Context, mediaQueries []*MediaMetadata) ([]*MediaMetadata, bool, error) {
	args := m.Called(ctx, mediaQueries)
	return args.Get(0).([]*MediaMetadata), args.Bool(1), args.Error(2)
}

func BuildSelectForPairs(mediaQueries []*MediaMetadata) (string, []interface{}) {
	return selectMediaByMediaIDs, slicendice.Map(mediaQueries, func(q *MediaMetadata, _ int) interface{} {
		return q.MediaVersionPair()
	})
}

func Test_BuildSelector(t *testing.T) {
	mediametadatas := []*MediaMetadata{
		{MediaID: "a1", Version: "v1"},
		{MediaID: "a2", Version: "v1"},
		{MediaID: "a1", Version: "v2"},
		{MediaID: "a3", Version: "v3"},
	}

	t.Run("creates the sql clause for a given pair of media_id and version", func(t *testing.T) {
		_, args := BuildSelectForPairs(mediametadatas)

		require.Equal(t, 4, len(args))

		assert.Equal(t, []interface{}{"(a1,v1)", "(a2,v1)", "(a1,v2)", "(a3,v3)"}, args)
	})
}
