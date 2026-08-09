-- information about the post processed media info
CREATE TABLE IF NOT EXISTS processed_media_metadatas (
  media_id VARCHAR(255) NOT NULL,
  source TEXT NOT NULL, -- original image blob store url
  version VARCHAR(25) NOT NULL,
  metadata JSONB NOT NULL,
  created_at TIMESTAMP DEFAULT NOW() NOT NULL,
  status VARCHAR(255) NOT NULL DEFAULT 'processing',
  processed_at TIMESTAMP DEFAULT NOW(),
  PRIMARY KEY (media_id, version)
);

CREATE TYPE media_version_pair AS (
  media varchar(255),
  version varchar(25)
);

CREATE INDEX IF NOT EXISTS idx_media_id ON processed_media_metadatas(media_id, version, status);
