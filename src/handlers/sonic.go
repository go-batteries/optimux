package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/go-batteries/optimux/src/config"
	md "github.com/go-batteries/optimux/src/services/mediametadata"
	"github.com/go-batteries/optimux/src/shared"
)

type MediaDistributionHandler struct {
	MetadataSvc *md.MediaMetadataService
}

const MaxAllowedPreload = 2000

func (m *MediaDistributionHandler) Handle(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mediaReqs := []*md.DistributionRequest{}

		if err := json.NewDecoder(r.Body).Decode(&mediaReqs); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		// establish sanity, maximum of 2000 preload allowed
		mediaReqs = mediaReqs[:MaxAllowedPreload]

		sort.Slice(mediaReqs, func(i, j int) bool {
			return mediaReqs[i].Prority < mediaReqs[j].Prority
		})

		var parts []string
		for _, req := range mediaReqs {
			part := fmt.Sprintf("<%s>; rel=\"preload\"; as=\"image\"", req.MediaURL)
			parts = append(parts, part)
		}

		// Join with comma
		linkHeader := strings.Join(parts, ", ")
		w.WriteHeader(http.StatusNoContent)
		w.Header().Add("Link", linkHeader)
	}
}

func (m *MediaDistributionHandler) HandleValidated(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mediaReqs := []*md.DistributionRequest{}

		if err := json.NewDecoder(r.Body).Decode(&mediaReqs); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		sort.Slice(mediaReqs, func(i, j int) bool {
			return mediaReqs[i].Prority < mediaReqs[j].Prority
		})

		ctx := r.Context()

		log.Println("sanitized requests", shared.Dumps(mediaReqs))

		results, err := m.MetadataSvc.FetchPreComputedFromRequest(ctx, mediaReqs...)
		if err != nil {
			http.Error(w, "something went wrong", http.StatusInternalServerError)
			return
		}

		if len(results) == 0 {
			w.Write([]byte(`{"data": []}`))
			return
		}

		u := r.Clone(r.Context()).URL
		u.Host = cfg.Domain.Host
		u.Scheme = cfg.Domain.Scheme

		responseURLs, err := md.BuildDistributionResponse(
			&cfg.Domain,
			mediaReqs,
			results,
		)
		if err != nil {
			http.Error(w, "something went wrong", http.StatusInternalServerError)
			return
		}

		log.Println("urls", shared.Dumps(responseURLs))

		shared.FlushResponse(w, func(w http.ResponseWriter) bool {
			// query := u.Query()
			linkHeaders := make([]string, 0, len(results)-1)

			// for _, size := range results[1:] {
			// 	query.Set("sizes", fmt.Sprintf("%dx%d", size[0], size[1]))
			// 	u.RawQuery = query.Encode()
			//
			// 	linkHeaders = append(linkHeaders, shared.BuildLinkHeaderFromStr(u.String(), "/optimux/"))
			// }

			// Combine Link headers in a single call
			w.Header().Add("Link", strings.Join(linkHeaders, ", "))
			return true
		})
	}
}
