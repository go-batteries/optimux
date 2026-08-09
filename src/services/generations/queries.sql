-- name: ListSuccessfullImageGeneration :many
SELECT 
	generation_id
	,media_id
	,output_media_path 
	,created_at
FROM generation 
WHERE type=$1
	AND status='success' 
	AND video_attributes IS NULL 
	AND created_at < $2 AND created_at >= $3
ORDER BY created_at DESC
LIMIT $4;

-- name: CountSuccessfullImageGeneration :one
SELECT 
	COUNT(1) as total
FROM generation 
WHERE type=$1
	AND status='success' 
	AND video_attributes IS NULL 
	AND created_at < $2 AND created_at >= $3;

-- name: ListSuccessfullImageGenerationByUser :many
SELECT 
	generation_id
	,media_id
	,output_media_path 
	,created_at
FROM generation 
WHERE type=$1
	AND user_id = $2
	AND status='success' 
	AND video_attributes IS NULL 
	AND created_at < $3 AND created_at >= $4
ORDER BY created_at DESC
LIMIT $5;

-- name: CountSuccessfullImageGenerationByUser :one
SELECT 
	COUNT(1) as total
FROM generation 
WHERE type=$1
	AND user_id = $2
	AND status='success' 
	AND video_attributes IS NULL 
	AND created_at < $3 AND created_at >= $4;
