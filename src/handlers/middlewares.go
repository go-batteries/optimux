package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-batteries/optimux/src/mediahose"
	"github.com/go-batteries/optimux/src/shared"
)

const (
	CorsHeaderAllowOrigin    = "Access-Control-Allow-Origin"
	CorsHeaderAllowMethods   = "Access-Control-Allow-Methods"
	CorsHeaderAllowHeaders   = "Access-Control-Allow-Headers"
	CorsHeaderExposedHeaders = "Access-Control-Expose-Headers"
	CorsHeaderAllowCreds     = "Access-Control-Allow-Credentials"
)

type CorsConfig struct {
	AllowedOrigins map[string]struct{}
	AllowedMethods string
	AllowedHeaders string
	ExposedHeaders string
	AllowCreds     bool
}

type ImageUrlExtractor func(r *http.Request) string

func ExtractFromPath(r *http.Request) string {
	return r.URL.Path
}

func ExtractFromQuery(r *http.Request) string {
	query := r.URL.Query()
	return query.Get("image_url")
}

const (
	CtxKeyMediaType = "media_type"
)

func ValidateExpectedFormat(next http.HandlerFunc) http.HandlerFunc {
	return ValidateExpectedFormatWithEnv(next, "")
}

func ValidateExpectedFormatWithEnv(next http.HandlerFunc, env string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		formatParam := r.URL.Query().Get("format")

		if formatParam == "" {
			next(w, r)
			return
		}

		format := fmt.Sprintf(".%s", formatParam)

		isImage := shared.IsOfMediaType(format, shared.AllowedImgExtMap)
		isVideo := shared.IsOfMediaType(format, shared.VideoExtMap)

		log.Println("ValidateFormat isImage", isImage, ", isVideo", isVideo, format)

		if !isImage && !isVideo {
			errMsg := fmt.Sprintf("❌ ValidateFormat: Unsupported format '%s' - not in AllowedImgExtMap or VideoExtMap", format)
			log.Println(errMsg)

			// In staging/dev, return detailed error
			if env == "stg" || env == "local" || env == "dev" || env == "stage" {
				http.Error(w, errMsg, http.StatusUnsupportedMediaType)
			} else {
				http.Error(w, "unsupported output media", http.StatusUnsupportedMediaType)
			}
			return
		}

		next(w, r)
	}
}

func ValidateAndSetMediaType(next http.HandlerFunc, pathExtractor ImageUrlExtractor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := pathExtractor(r)

		// If the file extension is not in the allowed extensions, proxy to S3
		isImage := shared.IsOfMediaType(path, shared.AllowedImgExtMap)
		isVideo := shared.IsOfMediaType(path, shared.VideoExtMap)

		log.Println("SetMediaType, isImage", isImage, ", isVideo", isVideo)

		if !isImage && !isVideo {
			errMsg := fmt.Sprintf("❌ SetMediaType: Path '%s' not recognized as image or video", path)
			log.Println(errMsg)

			// In staging/dev, return detailed error
			if os.Getenv("ENV") == "stg" || os.Getenv("ENV") == "local" {
				http.Error(w, errMsg, http.StatusUnsupportedMediaType)
			} else {
				http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			}
			return
		}

		ctx := r.Context()

		if isImage {
			ctx = context.WithValue(ctx, CtxKeyMediaType, mediahose.MediaTypeImage)
		} else {
			ctx = context.WithValue(ctx, CtxKeyMediaType, mediahose.MediaTypeVideo)
		}

		r = r.WithContext(ctx)

		query := r.URL.Query()

		r.URL.RawQuery = query.Encode()

		next(w, r)
	}
}

func NewCorsConfig(originsStr string) CorsConfig {
	origins := strings.Split(originsStr, ",")
	originMap := map[string]struct{}{}

	for _, origin := range origins {
		origin = strings.TrimSuffix(origin, "/")
		originMap[origin] = struct{}{}
	}

	return CorsConfig{
		AllowedOrigins: originMap,
		AllowedMethods: "GET, HEAD, POST, OPTIONS, PATCH",
		AllowedHeaders: "Authorization, Content-Type, X-Requested-With, X-CSRF-Token, Content-Disposition, Range",
		ExposedHeaders: "Content-Disposition, Content-Length, X-Request-Id, Content-Range, Accept-Ranges",
		AllowCreds:     true,
	}
}

func AddCorsHeaders(next http.HandlerFunc, corsCfg CorsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		originFromHeader := strings.TrimSuffix(r.Header.Get("Origin"), "/")

		_, ok := corsCfg.AllowedOrigins[originFromHeader]
		if !ok {
			// If Origin is not allowed but Allowed Origins has * in it, allow it
			_, ok = corsCfg.AllowedOrigins["*"]
		}

		if ok {
			w.Header().Add(CorsHeaderAllowOrigin, originFromHeader)
		}

		w.Header().Add(CorsHeaderAllowCreds, fmt.Sprintf("%v", corsCfg.AllowCreds))
		w.Header().Add(CorsHeaderAllowMethods, corsCfg.AllowedMethods)
		w.Header().Add(CorsHeaderExposedHeaders, corsCfg.ExposedHeaders)
		w.Header().Add(CorsHeaderAllowHeaders, corsCfg.AllowedHeaders)

		next(w, r)
	}
}

type Middleware func(http.HandlerFunc) http.HandlerFunc

func ChainMiddlewares(handler http.HandlerFunc, middlewares ...Middleware) http.HandlerFunc {
	// Apply each middleware in reverse order to the base handler
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
