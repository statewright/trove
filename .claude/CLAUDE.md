# trove

Lightweight S3-compatible object store. MIT licensed.

## Architecture

- `cmd/trove/` -- entry point, config loading, root key bootstrap
- `internal/auth/` -- AWS Signature V4 verification
- `internal/storage/` -- content-addressed filesystem blob store (SHA-256)
- `internal/metadata/` -- PostgreSQL metadata layer (buckets, objects, keys, multipart)
- `internal/s3api/` -- S3 HTTP API handlers and routing
- `internal/server/` -- HTTP server wrapper
- `deploy/` -- Helm chart and helmfile

## Key Design Decisions

- **Content-addressed blobs**: files stored by SHA-256 hash, not by S3 key. Gives dedup and safe RWX volume sharing for free.
- **PostgreSQL metadata**: all tables prefixed `trove_`. Can share a database with other applications (e.g., pgpocketbase).
- **Env-var only config**: no config files. Container-native.
- **Path-style and virtual-hosted**: both S3 URL styles supported.
- **Root key auto-generation**: printed to stderr on first run, persisted in `trove_access_keys`.

## Dependencies

- `github.com/jackc/pgx/v5` -- PostgreSQL driver
- No web framework -- stdlib `net/http` only

## Testing

- Unit tests: `go test ./internal/storage/ ./internal/auth/` (no PG needed)
- Full tests: `PG_TEST_URL="postgres://..." go test ./...`
- E2E: `docker compose up`, then AWS CLI commands

## S3 API Surface

Implements: PutObject, GetObject, HeadObject, DeleteObject, CopyObject, ListObjectsV2, CreateMultipartUpload, UploadPart, CompleteMultipartUpload, AbortMultipartUpload, CreateBucket, DeleteBucket, HeadBucket, ListBuckets.

Not implemented: versioning, object locking, lifecycle rules, bucket policies, SSE, ACLs.
