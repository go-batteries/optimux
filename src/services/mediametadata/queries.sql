-- name: SelectMediaByMediaIDs :many
SELECT
	media_id
	,source
	,version
	,metadata
	,created_at
	,processed_at
FROM processed_media_metadatas
WHERE (media_id, version) = ANY($1::media_version_pair[]) AND status = $2;
