package mediametadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/doug-martin/goqu/v9"
	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	"github.com/go-batteries/slicendice"
	"github.com/lib/pq"
	"github.com/roverxio/optimux/src/shared"
)

type SizeFormatTuple struct {
	Size        string  `json:"size"`
	Format      string  `json:"format"`
	Destination *string `json:"dest"`
}

func (st *SizeFormatTuple) Key() string {
	return fmt.Sprintf("%s_%s", st.Size, st.Format)
}

/*
"MetadataSchema": {
"path": "prod/user_generated/images/usr_2gZ984tlWx/img_ANTapDOn5z.png",
"sizes": [
{
"size": "@30w",
"format": ".webp",
"dest": null
}
*/
type V1MedatataSchema struct {
	OriginalPath string             `json:"path"`
	Bucket       string             `json:"bucket"`
	Sizes        []*SizeFormatTuple `json:"sizes"`
}

func (v1 *V1MedatataSchema) HasSize(reqSize string) bool {
	for _, size := range v1.Sizes {
		if size.Size == reqSize {
			return true
		}
	}

	return false
}

const (
	StateProcessing       string = "processing"
	StateProcessed        string = "success"
	StateProcessingFailed string = "failed"
)

type MediaMetadata struct {
	MediaID     string    `db:"media_id"`
	Source      string    `db:"source"`
	Version     string    `db:"version"`
	Metadata    []byte    `db:"metadata"`
	CreatedAt   time.Time `db:"created_at"`
	ProcessedAt time.Time `db:"processed_at"`
	Status      *string   `db:"status"`

	MetadataSchema *V1MedatataSchema `db:"-"`
}

func (m *MediaMetadata) MediaVersionPair() string {
	return fmt.Sprintf("(%s,%s)", m.MediaID, m.Version)
}

func (m *MediaMetadata) Index() string {
	return fmt.Sprintf("%s_%s", m.MediaID, m.Version)
}

func (m *MediaMetadata) ProcessedKeys() string {
	if m.MetadataSchema == nil {
		return ""
	}

	keys := make([]string, 0, len(m.MetadataSchema.Sizes))
	for _, tuple := range m.MetadataSchema.Sizes {
		if tuple != nil && tuple.Size != "" && tuple.Format != "" {
			keys = append(keys, fmt.Sprintf("%s%s", tuple.Size, tuple.Format))
		}
	}

	return strings.Join(keys, "_")
}

func CloneURL(r *url.URL) *url.URL {
	r2 := *r
	return &r2
}

func (m *MediaMetadata) Enrich() error {
	if m.MetadataSchema != nil {
		return errors.New("empty_metadata")
	}

	v1Schema := V1MedatataSchema{}

	// log.Println("schema text", m.MediaID, string(m.Metadata))

	err := json.Unmarshal(m.Metadata, &v1Schema)
	if err != nil {
		return err
	}

	m.MetadataSchema = &v1Schema

	for _, size := range m.MetadataSchema.Sizes {
		dir := filepath.Dir(v1Schema.OriginalPath)
		fileName := shared.AppendSizeToFileName(v1Schema.OriginalPath, size.Size, size.Format)

		size.Destination = shared.ToPtr(shared.GetResizedS3Key(dir, fileName))
	}

	// m.MetadataSchema.Sizes = append(
	// 	m.MetadataSchema.Sizes,
	// 	&SizeFormatTuple{
	// 		Size:        "@1x",
	// 		Format:      filepath.Ext(v1Schema.OriginalPath),
	// 		Destination: &v1Schema.OriginalPath,
	// 	},
	// )

	return nil
}

// Postgres Composite type support. but skippping
//
//	type MediaVersionPair struct {
//		Version string `db:"version"`
//		MediaID string `db:"media_id"`
//	}
//
// Called by worker to build media metadata for a given filePath
// using pre-existing size config.
func BuildV1MediaMetadataWithDefaults(filePath string) *MediaMetadata {
	fileName, ext, _ := shared.ExplodeFileName(filePath)

	sizeFormatMap := slicendice.Map(shared.SizesForCompression, func(size string, _ int) *SizeFormatTuple {
		return &SizeFormatTuple{Size: size, Format: ext}
	})

	// log.Println("v1 format", filePath, shared.Dumps(sizeFormatMap))

	return &MediaMetadata{
		MediaID:   fileName,
		Source:    "s3",
		Version:   "v1",
		CreatedAt: time.Now().UTC(),
		MetadataSchema: &V1MedatataSchema{
			OriginalPath: filePath,
			Sizes:        sizeFormatMap,
		},
	}
}

func BuildV1MediaMetadataFromUrl(urlStr string) (*MediaMetadata, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	return BuildV1MediaMetadataWithDefaults(u.Path), nil
}

type MediaMedataBuildOpts func(m *MediaMetadata)

func WithV1MetadataSchema(v1Schema *V1MedatataSchema) MediaMedataBuildOpts {
	return func(m *MediaMetadata) {
		m.MetadataSchema = v1Schema
	}
}

func BuildMediaMetadata(filePath string, opts ...MediaMedataBuildOpts) (*MediaMetadata, error) {
	fileName, _, ok := shared.ExplodeFileName(filePath)
	if !ok {
		return nil, errors.New("invalid filepath, filename.ext format missing")
	}

	metadata := &MediaMetadata{
		MediaID:   fileName,
		Source:    "s3",
		Version:   "v1",
		CreatedAt: time.Now().UTC(),
	}

	for _, opt := range opts {
		opt(metadata)
	}

	return metadata, nil
}

type MediaMetadataRepo struct {
	DB           *sql.DB
	QueryBuilder goqu.DialectWrapper
	// Conn         *pgx.Conn
}

func (MediaMetadata) Table() string {
	return "processed_media_metadatas"
}

func NewMediaMetadataRepo(dsn string, db *sql.DB) *MediaMetadataRepo {
	m := &MediaMetadataRepo{
		DB:           db,
		QueryBuilder: goqu.Dialect(dsn),
		// Conn:         conn,
	}

	m.QueryBuilder.DB(db)
	return m
}

type Repository interface {
	Create(ctx context.Context, data []*MediaMetadata) error
	UpdateSizes(ctx context.Context, data *MediaMetadata) error
	SelectMedias(ctx context.Context, mediaQueries []*MediaMetadata) ([]*MediaMetadata, bool, error)
}

func (ar *MediaMetadataRepo) Create(ctx context.Context, data []*MediaMetadata) error {
	if len(data) == 0 {
		return nil
	}

	table := data[0].Table()
	ds := ar.QueryBuilder.Insert(table).Rows(data).OnConflict(goqu.DoNothing())

	stmt, args, err := ds.Prepared(true).ToSQL()
	if err != nil {
		return fmt.Errorf("failed to build insert query. error %v", err)
	}

	log.Println("CreateMediaMetadata", stmt)

	tx, err := ar.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction. err %v", err)
	}

	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to insert record. err %v", err)
	}

	return tx.Commit()
}

func (ar *MediaMetadataRepo) UpdateSizes(ctx context.Context, data *MediaMetadata) error {
	ds := ar.QueryBuilder.Update(data.Table()).Set(goqu.Record{
		"metadata": data.Metadata,
	}).Where(goqu.Ex{"media_id": data.MediaID})

	stmt, args, err := ds.Prepared(true).ToSQL()
	if err != nil {
		return fmt.Errorf("failed to build insert query. error %v", err)
	}

	log.Println("UpdateSizes", stmt)

	if _, err := ar.DB.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("failed to update sizes for media id. %s. error: %v", data.MediaID, err)
	}

	return nil
}

func (ar *MediaMetadataRepo) SelectMedias(ctx context.Context, mediaQueries []*MediaMetadata) ([]*MediaMetadata, bool, error) {
	if len(mediaQueries) == 0 {
		return []*MediaMetadata{}, false, nil
	}

	pairs := slicendice.Map(mediaQueries, func(q *MediaMetadata, _ int) string {
		return q.MediaVersionPair()
	})

	log.Print(selectMediaByMediaIDs)
	log.Println(pairs)

	q := New(ar.DB)
	rows, err := q.db.QueryContext(ctx, selectMediaByMediaIDs, pq.Array(pairs), StateProcessed)
	if err != nil {
		log.Println("failed to get media metadata", err)
		return nil, false, err
	}

	defer rows.Close()
	hasError := false

	results := []*MediaMetadata{}
	for rows.Next() {
		var result MediaMetadata

		if err = rows.Scan(
			&result.MediaID,
			&result.Source,
			&result.Version,
			&result.Metadata,
			&result.CreatedAt,
			&result.ProcessedAt,
		); err != nil {
			log.Printf("failed to get records. error %v", err)
			hasError = true
			continue
		}

		results = append(results, &result)
	}

	log.Println("total results", len(results))

	return results, hasError, err
}

func JoinSchema(all []*MediaMetadata) []*MediaMetadata {
	type key = string
	merged := make(map[key]*MediaMetadata)

	for _, meta := range all {
		baseID := meta.MediaID

		if existing, found := merged[baseID]; found {
			existing.MetadataSchema.Sizes = append(existing.MetadataSchema.Sizes, meta.MetadataSchema.Sizes...)
		} else {
			// Clone the original to avoid modifying shared pointers
			copied := *meta
			copied.MetadataSchema.Sizes = append([]*SizeFormatTuple{}, meta.MetadataSchema.Sizes...)
			merged[baseID] = &copied
		}
	}

	result := make([]*MediaMetadata, 0, len(merged))
	for _, m := range merged {
		result = append(result, m)
	}
	return result
}

// func (ar *MediaMetadataRepo) SelectMedias3(ctx context.Context, mediaQueries []*MediaMetadata) ([]*MediaMetadata, bool, error) {
// 	if len(mediaQueries) == 0 {
// 		return []*MediaMetadata{}, false, nil
// 	}
//
// 	query, args := BuildSelectForPairs(QuerySelectMediaMetadataByMediaID, mediaQueries)
//
// 	log.Println(query)
//
// 	rows, err := ar.DB.QueryContext(
// 		ctx,
// 		query,
// 		args...,
// 	)
// 	if err != nil {
// 		log.Println("failed to execute query", err)
//
// 		return nil, false, err
// 	}
//
// 	defer rows.Close()
// 	hasError := false
//
// 	results := []*MediaMetadata{}
// 	for rows.Next() {
// 		var result MediaMetadata
//
// 		if err = rows.Scan(
// 			&result.MediaID,
// 			&result.Source,
// 			&result.Version,
// 			&result.Metadata,
// 			&result.CreatedAt,
// 			&result.ProcessedAt,
// 		); err != nil {
// 			log.Printf("failed to get records. error %v", err)
// 			hasError = true
// 			continue
// 		}
//
// 		results = append(results, &result)
// 	}
//
// 	log.Println("total results", len(results))
//
// 	return results, hasError, err
// }

// const (
// 	// 	QuerySelectMediaMetadataByMediaID = `
// 	//
// 	// SELECT
// 	// 	media_id
// 	// 	,source
// 	// 	,version
// 	// 	,metadata
// 	// 	,created_at
// 	// 	,processed_at
// 	// FROM processed_media_metadatas
// 	// WHERE (media_id, version) IN (
// 	// 	%s
// 	// )
// 	// 	`
// 	QuerySelectMediaMetadataByMediaID = `
//
// SELECT
// 	media_id
// 	,source
// 	,version
// 	,metadata
// 	,created_at
// 	,processed_at
// FROM processed_media_metadatas
// WHERE (media_id, version) = ANY($1::media_version_pair[])`
// )
//
// func BuildSelectForPairs(queryPrefix string, pairs []*MediaMetadata) (string, []interface{}) {
// 	results := []string{}
// 	args := []interface{}{}
//
// 	for i := 0; i < len(pairs); i++ {
// 		results = append(results, "(?,?)")
// 		args = append(args, []interface{}{pairs[i].MediaID, pairs[i].Version}...)
// 	}
//
// 	stmt := strings.Join(results, ",")
// 	query := fmt.Sprintf(queryPrefix, stmt)
//
// 	return query, args
// }
