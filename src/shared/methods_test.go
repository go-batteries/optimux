package shared

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_ExtraceSizeID(t *testing.T) {
	t.Run("should extract the correct sizeID from post processed url", func(t *testing.T) {
		sizeID := ExtractSizeIDFromFile("img_1234242@2x.webp")

		require.Equal(t, "2x", sizeID)
	})

	t.Run("should return blank sizeID, if not present", func(t *testing.T) {
		sizeID := ExtractSizeIDFromFile("img")

		require.Equal(t, "", sizeID)
	})
}

func Test_ToSizeStr(t *testing.T) {
	t.Run("with no dimension, size to str fails", func(t *testing.T) {
		_, ok := ToSizeStr()
		require.False(t, ok)
	})

	t.Run("with 0x0 dimension, size to str fails", func(t *testing.T) {
		_, ok := ToSizeStr(0, 0)

		require.False(t, ok)
	})

	t.Run("with 0x dimension, size to str fails", func(t *testing.T) {
		_, ok := ToSizeStr(0)

		require.False(t, ok)
	})

	t.Run("with only with, returns on width", func(t *testing.T) {
		v, ok := ToSizeStr(600)

		require.True(t, ok)
		require.Equal(t, "600x", v)
	})

	t.Run("with only height, returns on height", func(t *testing.T) {
		v, ok := ToSizeStr(0, 800)

		require.True(t, ok)
		require.Equal(t, "x800", v)
	})

	t.Run("with width height, returns both", func(t *testing.T) {
		v, ok := ToSizeStr(600, 800)

		require.True(t, ok)
		require.Equal(t, "600x800", v)
	})
}
