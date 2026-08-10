package registry

import (
	"context"
	"errors"
	"log"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/go-batteries/optimux/src/config"
	"github.com/go-batteries/optimux/src/shared"
)

const (
	MediaProviderS3 = "s3"
)

type Timestamps struct {
	CreatedAt time.Time  `db:"created_at"`
	UpdatedAt time.Time  `db:"updated_at"`
	DeletedAt *time.Time `db:"deleted_at"`
}

type S3MediaProviderConfig struct {
	BucketPrefix string `json:"bucket_prefix"`
	BaseURL      string `json:"base_url"`
	Service      string `json:"string"`
}

type APISourceConfig struct {
	BaseURL      string  `json:"base_url"`
	OxPathPrefix string  `json:"optimux_path_prefix"`
	StatusCodes  string  `json:"status_codes"`
	Query        *string `json:"query"`
	Headers      *string `json:"headers"`
}

func ModelValidator(model any) error {
	validate := validator.New()
	return validate.Struct(model)
}

type SourceRegistry interface {
	List(ctx context.Context) ([]*OptimuxSourceRecord, error)
	Match(ctx context.Context, path string) (*OptimuxSourceRecord, error)
}

type OptimuxSourceRecord struct {
	Slug               string  `db:"slug" validate:"required"`
	SourceType         string  `db:"source_type" validate:"required"`
	Source             string  `db:"source" validate:"required"`
	SourceConfig       *string `db:"source_config"`
	MediaExtractor     string  `db:"extract_with" validate:"required"`
	MediaStoreProvider string  `db:"provider" validate:"required"`
	Service            string  `db:"service" validate:"required"`
	MediaStoreConfig   *string `db:"provider_config" validate:"required"`

	*Timestamps

	// Derived Fields

	*S3MediaProviderConfig `db:"-" json:"-"`
	*APISourceConfig       `db:"-" json:"-"`
}

type OptimuxSourceRegistry struct {
	records []*OptimuxSourceRecord
}

func (r *OptimuxSourceRegistry) Match(ctx context.Context, path string) (*OptimuxSourceRecord, error) {
	// TODO: replace with Trie
	for _, record := range r.records {
		if strings.HasPrefix(path, record.Source) {
			return record, nil
		}
	}

	return nil, errors.New("no matching record found")
}

func (r *OptimuxSourceRegistry) List(ctx context.Context) ([]*OptimuxSourceRecord, error) {
	return r.records, nil
}

func DefaultSourceRegistry(cfg *config.Config) *OptimuxSourceRegistry {
	baseurl := cfg.SourceAPIBaseURL

	apiSourceConfig := &APISourceConfig{
		BaseURL:      baseurl,
		OxPathPrefix: "/optimux/assets",
	}

	registries := []*OptimuxSourceRecord{
		{
			Slug:               "canvas",
			SourceType:         "api",
			Service:            "feed",
			Source:             "/stg/v1/media/canvas/feed",
			SourceConfig:       shared.ToPtr(shared.MustJsonMarshall(map[string]string{"base_url": baseurl, "optimux_path_prefix": "optimux/assets/"})),
			MediaExtractor:     "gjson://data.#.media.#.output",
			MediaStoreProvider: MediaProviderS3,
			MediaStoreConfig: shared.ToPtr(
				shared.MustJsonMarshall(map[string]string{
					"bucket_prefix": "stg",
					"base_url":      cfg.S3BaseURL,
				}),
			), // This is to be replaced using ENV
			S3MediaProviderConfig: &S3MediaProviderConfig{
				BucketPrefix: "stg",
				BaseURL:      cfg.S3BaseURL,
				Service:      "feed",
			},
			APISourceConfig: apiSourceConfig,
		},
		{
			Slug:               "studio",
			SourceType:         "api",
			Service:            "feed",
			Source:             "/stg/v1/media/studio/feed",
			SourceConfig:       shared.ToPtr(shared.MustJsonMarshall(map[string]string{"base_url": baseurl, "optimux_path_prefix": "optimux/assets/"})),
			MediaExtractor:     "gjson://data.#.media.#.output",
			MediaStoreProvider: MediaProviderS3,
			MediaStoreConfig: shared.ToPtr(
				shared.MustJsonMarshall(map[string]string{
					"bucket_prefix": "stg",
					"base_url":      cfg.S3BaseURL,
				}),
			),
			S3MediaProviderConfig: &S3MediaProviderConfig{
				BucketPrefix: "stg",
				BaseURL:      cfg.S3BaseURL,
				Service:      "feed",
			},
			APISourceConfig: apiSourceConfig,
		},
		{
			Slug:               "canvas",
			SourceType:         "api",
			Service:            "feed",
			Source:             "/app/v1/media/canvas/feed",
			SourceConfig:       shared.ToPtr(shared.MustJsonMarshall(map[string]string{"base_url": baseurl, "optimux_path_prefix": "optimux/assets/"})),
			MediaExtractor:     "gjson://data.#.media.#.output",
			MediaStoreProvider: MediaProviderS3,
			MediaStoreConfig: shared.ToPtr(
				shared.MustJsonMarshall(map[string]string{
					"bucket_prefix": "prod",
					"base_url":      cfg.S3BaseURL,
				}),
			),
			S3MediaProviderConfig: &S3MediaProviderConfig{
				BucketPrefix: "prod",
				BaseURL:      cfg.S3BaseURL,
				Service:      "feed",
			},
			APISourceConfig: apiSourceConfig,
		},
		{
			Slug:               "studio",
			SourceType:         "api",
			Service:            "feed",
			Source:             "/app/v1/media/studio/feed",
			SourceConfig:       shared.ToPtr(shared.MustJsonMarshall(map[string]string{"base_url": baseurl, "optimux_path_prefix": "optimux/assets/"})),
			MediaExtractor:     "gjson://data.#.media.#.output",
			MediaStoreProvider: MediaProviderS3,
			MediaStoreConfig: shared.ToPtr(
				shared.MustJsonMarshall(map[string]string{
					"bucket_prefix": "prod",
					"base_url":      cfg.S3BaseURL,
				}),
			),
			S3MediaProviderConfig: &S3MediaProviderConfig{
				BucketPrefix: "prod",
				BaseURL:      cfg.S3BaseURL,
				Service:      "feed",
			},
			APISourceConfig: apiSourceConfig,
		},
	}

	return &OptimuxSourceRegistry{
		records: registries,
	}
}

func TransformS3URLs(cfg *config.Config, imageURLs []string, sizes []string, record *OptimuxSourceRecord) []*url.URL {
	var transformed []*url.URL

	bucketPrefix := shared.MustBeginStr(record.S3MediaProviderConfig.BucketPrefix, "/")
	newPrefix := record.APISourceConfig.OxPathPrefix

	for _, imgURL := range imageURLs {
		u, err := url.Parse(imgURL)
		if err != nil {
			log.Println("failed to parse transformed url", imgURL)
			continue
		}

		path := u.Path

		if record.Service == "feed" && strings.HasPrefix(path, bucketPrefix) {
			u.Host = cfg.Domain.Host
			u.Scheme = cfg.Domain.Scheme

			path = strings.TrimLeft(path, bucketPrefix)
			path = filepath.Join(newPrefix, path)

			u.Path = path
		}

		if len(sizes) > 0 {
			query := u.Query()

			for _, size := range sizes {
				query.Add("sizes", size)
			}

			u.RawQuery = query.Encode()
		}

		transformed = append(transformed, u)
	}

	return transformed
}
