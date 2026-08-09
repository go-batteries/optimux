package shared

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_ClampQuality(t *testing.T) {
	tests := []struct {
		name     string
		values   [3]int
		expected int
	}{
		{
			name:     "returns original value when inside range",
			values:   [3]int{0, 81, 75},
			expected: 75,
		},
		{
			name:     "return max value, when outside max range",
			values:   [3]int{0, 81, 90},
			expected: 81,
		},
		{
			name:     "return min value, when outside min range",
			values:   [3]int{10, 81, 0},
			expected: 10,
		},
		{
			name:     "interchanges min and max when shit happens",
			values:   [3]int{80, 10, 74},
			expected: 74,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res := ClampValue(
				test.values[0],
				test.values[1],
				test.values[2],
			)

			require.Equal(t, test.expected, res)
		})
	}
}

func Test_MaybeDecodeURI(t *testing.T) {
	t.Run("converts % symbols in path to characters", func(t *testing.T) {
		res := MaybeDecodeURI("user_generated/usr_ezjjIL2JUq/img_0ZsJnB5E010_%402x.webp")

		require.Equal(t, "user_generated/usr_ezjjIL2JUq/img_0ZsJnB5E010_@2x.webp", res)
	})

	t.Run("returns as is, if conversion is not possible", func(t *testing.T) {
		res := MaybeDecodeURI("user_generated/usr_ezjjIL2JUq/img_0ZsJnB5E010_@2x.webp")

		require.Equal(t, "user_generated/usr_ezjjIL2JUq/img_0ZsJnB5E010_@2x.webp", res)
	})
}
