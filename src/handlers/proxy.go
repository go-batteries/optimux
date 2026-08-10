package handlers

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-batteries/slicendice"
	"github.com/go-batteries/optimux/src/config"
	"github.com/go-batteries/optimux/src/registry"
	"github.com/go-batteries/optimux/src/services/mediametadata"
	"github.com/go-batteries/optimux/src/shared"
	"golang.org/x/net/http2"
)

type ProxyHandler struct {
	Reg         registry.SourceRegistry
	Enabled     bool
	MetadataSvc *mediametadata.MediaMetadataService
}

// newHTTP2Transport creates an HTTP/2 transport that supports both TLS (HTTPS) and h2c (cleartext HTTP/2)
func newHTTP2Transport() *http.Transport {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	// Enable HTTP/2 support in transport
	err := http2.ConfigureTransport(transport)
	if err != nil {
		log.Fatalf("Failed to configure HTTP/2 transport: %v", err)
	}

	return transport
}

// newH2CTransport creates an HTTP/2 Cleartext (h2c) transport
func newH2CTransport() *http2.Transport {
	return &http2.Transport{
		AllowHTTP: true, // Allow HTTP/2 without TLS (h2c)
		DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
			// Establish a cleartext (h2c) connection
			return net.Dial(network, addr)
		},
	}
}

type ReverseProxy interface{}

func (s *ProxyHandler) Handle(quality int, cfg *config.Config, stripPath string, shouldTransform bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer shared.Bench("Proxying media requests")()

		if !s.Enabled {
			w.WriteHeader(http.StatusTeapot)
			w.Write([]byte("go away"))
			return
		}

		ctx := r.Context()

		urlPath := r.URL.Path

		log.Println("urlpath", urlPath, stripPath)

		if strings.HasPrefix(urlPath, stripPath) {
			urlPath = strings.TrimPrefix(urlPath, stripPath)
		}

		urlPath = shared.MustBeginStr(urlPath, "/")
		log.Println("trying to find matching config for path", urlPath)

		record, err := s.Reg.Match(ctx, urlPath)
		if err != nil {
			http.Error(w, "No matching API source", http.StatusNotFound)
			return
		}

		backendURL, err := url.Parse(record.APISourceConfig.BaseURL)
		if err != nil {
			http.Error(
				w,
				"E602 something went wrong",
				http.StatusInternalServerError,
			)

			return
		}

		log.Println("proxying request to", backendURL)

		query := r.URL.Query()

		prefetchCountParams := query.Get("prefetch")
		prefetchCount := cfg.MaxPrefetch

		if prefetchCountParams != "" {
			count, err := strconv.Atoi(prefetchCountParams)
			if err == nil {
				prefetchCount = count
			}
		}

		sizes, _ := query["sizes"]

		queriedSizes := slicendice.ToSet(sizes...).ToList()

		if len(sizes) == 0 {
			sizes = []string{"@1x"}
		}

		log.Println("requesting for sizes, before", sizes)

		// Convert the size mapping to post processed mapping
		for i, size := range sizes {
			if v, ok := shared.IdSizeMapRev[size]; ok {
				sizes[i] = v
			}
		}

		sizes = slicendice.ToSet(sizes...).ToList()

		log.Println("requesting for sizes, after", sizes)

		extractorQuery := record.MediaExtractor

		// Strategy pattern depending on the protocol, for laters
		if strings.HasPrefix(extractorQuery, "gjson://") {
			extractorQuery = strings.TrimLeft(extractorQuery, "gjson://")
		} else {
			log.Println("unsupported extractor", extractorQuery)
		}

		log.Println("using extractor query", extractorQuery)

		// Modify request
		// TODO: put this behind an interface
		proxy := httputil.NewSingleHostReverseProxy(backendURL)
		proxy.ErrorHandler = RProxyErrorHandler()

		proxy.Transport = newHTTP2Transport()
		proxy.Director = func(r *http.Request) {
			r.URL.Path = urlPath
			r.URL.Scheme = backendURL.Scheme
			r.URL.Host = backendURL.Host

			log.Println("sending request to ", r.URL.String())

			if r.ProtoMajor == 2 && r.TLS == nil {
				proxy.Transport = newH2CTransport()
			}
		}

		proxy.ModifyResponse = func(resp *http.Response) error {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			imageURLs := shared.ExtractDataFromJson(
				string(body),
				extractorQuery,
			)

			// TODO: use the media_id (filename.ext), to find the available sizes
			// expect the query to have a priority, which will determine given the available sizes
			// which one to return
			// It should also modify the response to add a "srcset" field.
			// which should be passed from the record config.
			// take a gjson, and append a key value.
			maxPrefetch := shared.ClampValue(0, len(imageURLs), prefetchCount)

			log.Printf("extracted %d and clamped %d image urls", len(imageURLs), maxPrefetch)

			imageURLs = imageURLs[0:maxPrefetch]

			// Transform image URLs
			transformedURLs := registry.TransformS3URLs(cfg, imageURLs, queriedSizes, record)

			// log.Println("transfoemed", transformedURLs)

			mediaReqs := slicendice.MapFilter(transformedURLs, func(u *url.URL, _ int) (
				*mediametadata.DistributionRequest, bool,
			) {
				m, err := mediametadata.BuildDistributionReqFromUrl(u)
				if err != nil {
					log.Println("fucked", err)
				}
				return m, err == nil
			})

			// log.Printf("transformed %d urls", len(transformedURLs))

			// log.Println(shared.Dumps(mediaReqs))

			results, err := s.MetadataSvc.FetchPreComputedFromRequest(ctx, mediaReqs...)
			if err != nil {
				return errors.New("something went wrong")
			}

			_ = results
			// log.Println(shared.Dumps(results))

			// Filter by sizes

			if len(transformedURLs) > 0 {
				header := shared.BuildAllLinkHeader(transformedURLs)

				log.Println("header", header)
				resp.Header.Set("Link", header)
			}

			resp.Body = io.NopCloser(bytes.NewReader(body))

			return nil
		}

		proxy.ServeHTTP(w, r)
	}
}

// type S3ProxyHandler struct {}

func RProxyErrorHandler() func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("failed to serve using proxy. error %v", err)

		http.Error(
			w,
			"Got error trying to do weird stuff",
			http.StatusInternalServerError,
		)
		return
	}
}
