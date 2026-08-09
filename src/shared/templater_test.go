package shared

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_TemplateStringParsing(t *testing.T) {
	tests := map[string]string{
		"/user_generated/{user_id}/{filename}.{ext}": "user_generated/usr1234/blblbla.webp",
		"user_generated/{user_id}/{filename}.{ext}":  "/user_generated/usr1234/somefile.png",
		"/user_generated/{user_id}/*":                "user_generated/usr1234/any/path/here.jpg",
	}

	for template, path := range tests {
		pattern := TemplateToRegex(template)
		params := ExtractParams(pattern, path)

		fmt.Println(params)
		usr_id, ok := params["user_id"]
		require.True(t, ok)
		require.Equal(t, usr_id, "usr1234")
	}
}
