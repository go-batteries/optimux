package registry

import (
	"net/url"
	"strings"
	"testing"

	"github.com/go-batteries/optimux/src/config"
	"github.com/stretchr/testify/require"
)

func Test_TransformS3Urls(t *testing.T) {
	urls := []string{
		"https://redernet-image-data.s3.amazonaws.com/stg/user_generated/images/usr_Y61VR1rGQh/img_aav9iua982.png",
		"https://redernet-image-data.s3.amazonaws.com/stg/user_generated/images/usr_Y61VR1rGQh/img_ffLM8BbTud.png",
		"https://redernet-image-data.s3.amazonaws.com/stg/user_generated/images/usr_Y61VR1rGQh/img_Pj6gwzQxg3.png",
		"https://redernet-image-data.s3.amazonaws.com/stg/user_generated/images/usr_Y61VR1rGQh/img_mi4KbeTpDx.png",
	}

	u, err := url.Parse("https://example.com")
	require.NoError(t, err)

	tUrls := TransformS3URLs(&config.Config{Domain: *u},
		urls,
		[]string{},
		&OptimuxSourceRecord{
			Service: "feed",
			S3MediaProviderConfig: &S3MediaProviderConfig{
				BucketPrefix: "stg",
				BaseURL:      "",
			},
			APISourceConfig: &APISourceConfig{
				OxPathPrefix: "/optimux/assets",
			},
		})

	require.Equal(t, len(urls), len(tUrls))
	require.True(
		t,
		strings.HasPrefix(
			tUrls[0].String(),
			"https://example.com/optimux/assets/user_generated",
		),
	)
}
