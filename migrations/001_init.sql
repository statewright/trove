CREATE TABLE IF NOT EXISTS trove_buckets (
    name       TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS trove_objects (
    bucket       TEXT NOT NULL REFERENCES trove_buckets(name) ON DELETE CASCADE,
    key          TEXT NOT NULL,
    size         BIGINT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    etag         TEXT NOT NULL,
    blob_hash    TEXT NOT NULL,
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bucket, key)
);

CREATE INDEX IF NOT EXISTS trove_objects_prefix_idx
    ON trove_objects (bucket, key text_pattern_ops);

CREATE TABLE IF NOT EXISTS trove_multipart_uploads (
    upload_id  TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    bucket     TEXT NOT NULL,
    key        TEXT NOT NULL,
    metadata   JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS trove_multipart_parts (
    upload_id   TEXT NOT NULL REFERENCES trove_multipart_uploads(upload_id) ON DELETE CASCADE,
    part_number INT NOT NULL,
    size        BIGINT NOT NULL,
    etag        TEXT NOT NULL,
    blob_hash   TEXT NOT NULL,
    PRIMARY KEY (upload_id, part_number)
);

CREATE TABLE IF NOT EXISTS trove_access_keys (
    access_key_id TEXT PRIMARY KEY,
    secret_key    TEXT NOT NULL,
    display_name  TEXT NOT NULL DEFAULT '',
    is_root       BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS trove_bucket_permissions (
    access_key_id TEXT NOT NULL REFERENCES trove_access_keys(access_key_id) ON DELETE CASCADE,
    bucket        TEXT NOT NULL REFERENCES trove_buckets(name) ON DELETE CASCADE,
    can_read      BOOLEAN NOT NULL DEFAULT false,
    can_write     BOOLEAN NOT NULL DEFAULT false,
    is_owner      BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (access_key_id, bucket)
);
