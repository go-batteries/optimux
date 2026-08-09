package shared

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type DoneCh chan struct{}

type MailBoxCh chan interface{}

func ToPtr[E any](v E) *E {
	return &v
}

func FromPtr[E any](v *E) E {
	var empty E

	if v == nil {
		return empty
	}

	return *v
}

func MustJsonMarshall(e any) string {
	b, err := json.Marshal(e)
	if err != nil {
		log.Println("failed to marshal to json", e, err)
		return fmt.Sprintf("%v", e)
	}

	return string(b)
}

func ExtractDataFromJson(jsonBody string, gjsonPath string) []string {
	var urls []string
	results := gjson.Get(jsonBody, gjsonPath)

	results.ForEach(func(_, value gjson.Result) bool {
		if !value.IsArray() {
			urls = append(urls, value.String())
			return true
		}

		value.ForEach(func(_, v gjson.Result) bool {
			urls = append(urls, v.String())
			return true
		})

		return true
	})

	return urls
}

func MustBeginStr(str string, prefix string, prefixes ...string) string {
	if strings.HasPrefix(str, prefix) {
		return str
	}

	if len(prefixes) == 0 {
		return fmt.Sprintf("%s%s", prefix, str)
	}

	prefixes = append([]string{prefix}, prefixes...)
	fullPrefix := strings.Join(append(prefixes, str), "")
	return fullPrefix
}

func ClampValue(min, max, got int) int {
	if min > max { // when shit happens
		min, max = max, min
	}

	if got > min && got <= max {
		return got
	}

	if got > max {
		return max
	}

	return min
}

func MaybeDecodeURI(encodedURI string) string {
	decoded, err := url.PathUnescape(encodedURI)
	if err != nil {
		return encodedURI
	}

	return decoded
}

func GenerateRandomInt() int {
	return rand.New(rand.NewSource(time.Now().UnixNano())).Int()
}

// FormatTimestamp formats seconds to WebVTT timestamp format (HH:MM:SS.mmm)
func FormatTimestamp(seconds float64) string {
	hours := int(seconds) / 3600
	minutes := (int(seconds) % 3600) / 60
	secs := int(seconds) % 60
	millis := int((seconds - float64(int(seconds))) * 1000)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, secs, millis)
}
