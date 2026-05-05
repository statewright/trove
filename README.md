# Trove

Lightweight S3-compatible object store. MIT licensed.

PostgreSQL for metadata, content-addressed filesystem for blobs. Single binary, env-var config, 28MB Docker image.

## Quick Start

### Docker Compose

```bash
docker compose up -d
```

This starts Trove + PostgreSQL. Root credentials are printed to stderr on first run.

### Binary

```bash
export TROVE_POSTGRES_URL="postgres://user:pass@localhost:5432/trove?sslmode=disable"
go build -o trove ./cmd/trove
./trove
```

### Verify

```bash
# Create a bucket
aws --endpoint-url http://localhost:9000 s3 mb s3://my-bucket

# Upload
echo "hello" | aws --endpoint-url http://localhost:9000 s3 cp - s3://my-bucket/hello.txt

# Download
aws --endpoint-url http://localhost:9000 s3 cp s3://my-bucket/hello.txt -
```

## Configuration

All configuration via environment variables.

| Variable | Default | Description |
|----------|---------|-------------|
| `TROVE_LISTEN` | `:9000` | HTTP listen address |
| `TROVE_POSTGRES_URL` | *(required)* | PostgreSQL connection string |
| `TROVE_DATA_DIR` | `./data` | Blob storage directory |
| `TROVE_ROOT_ACCESS_KEY` | *(auto-generated)* | Root access key ID |
| `TROVE_ROOT_SECRET_KEY` | *(auto-generated)* | Root secret key |

Root credentials are auto-generated and printed to stderr on first run if not provided. They are stored in the `trove_access_keys` table and persist across restarts.

## S3 API Compatibility

Trove implements the S3 REST API subset used by common S3 clients (AWS CLI, AWS SDKs, PocketBase, rclone).

| Operation | Method | Path |
|-----------|--------|------|
| ListBuckets | `GET /` | |
| CreateBucket | `PUT /{bucket}` | |
| HeadBucket | `HEAD /{bucket}` | |
| DeleteBucket | `DELETE /{bucket}` | |
| PutObject | `PUT /{bucket}/{key}` | |
| GetObject | `GET /{bucket}/{key}` | Range requests supported |
| HeadObject | `HEAD /{bucket}/{key}` | |
| DeleteObject | `DELETE /{bucket}/{key}` | |
| CopyObject | `PUT /{bucket}/{key}` | `x-amz-copy-source` header |
| ListObjectsV2 | `GET /{bucket}?list-type=2` | prefix, delimiter, pagination |
| CreateMultipartUpload | `POST /{bucket}/{key}?uploads` | |
| UploadPart | `PUT /{bucket}/{key}?uploadId&partNumber` | |
| CompleteMultipartUpload | `POST /{bucket}/{key}?uploadId` | |
| AbortMultipartUpload | `DELETE /{bucket}/{key}?uploadId` | |

**Authentication:** AWS Signature Version 4. Both path-style (`/bucket/key`) and virtual-hosted-style (`bucket.host/key`) URLs.

**Not implemented:** versioning, object locking, lifecycle rules, bucket policies, server-side encryption, ACLs. These are deferred, not rejected — contributions welcome.

## Architecture

```
┌──────────────┐     ┌──────────────┐
│  S3 Client   │────▶│    Trove     │
│  (AWS CLI,   │     │  (HTTP API)  │
│   PocketBase,│     │              │
│   rclone)    │     └──┬───────┬───┘
└──────────────┘        │       │
                        ▼       ▼
              ┌─────────────┐ ┌─────────────┐
              │ PostgreSQL  │ │ Filesystem  │
              │ (metadata)  │ │ (blobs)     │
              └─────────────┘ └─────────────┘
```

**Metadata (PostgreSQL):** Bucket definitions, object keys, ETags, content types, multipart upload state, access keys. All tables prefixed `trove_` so Trove can share a database with other applications.

**Blobs (filesystem):** Content-addressed by SHA-256. Stored at `{data_dir}/blobs/{hash[0:2]}/{hash[2:4]}/{hash}`. Identical content is stored once regardless of how many objects reference it.

**Why content-addressed?**
- Deduplication for free
- Safe on shared filesystems (NFS, Longhorn, etc.) — same hash = same bytes = idempotent writes
- Multiple Trove replicas can share the same volume without coordination

## Scaling

Trove is stateless at the application level. All state lives in PostgreSQL (metadata) and the filesystem (blobs).

**Horizontal scaling:** Run multiple Trove replicas behind a load balancer. Point them at the same PostgreSQL instance and the same RWX (ReadWriteMany) volume. No configuration changes needed.

```yaml
# Kubernetes: scale replicas
persistence:
  accessModes: [ReadWriteMany]
  storageClass: your-rwx-class   # NFS, Longhorn, etc.
replicaCount: 3
```

## Kubernetes

Helm chart included in `deploy/chart/`. Helmfile example in `deploy/helmfile.yaml`.

```bash
# Direct Helm install
helm install trove deploy/chart \
  --set postgres.url="postgres://user:pass@pg:5432/trove?sslmode=disable" \
  --set persistence.storageClass=your-storage-class

# Or via helmfile
cd deploy && helmfile apply
```

Credentials can be provided inline or via Kubernetes secrets:

```yaml
postgres:
  existingSecret: trove-secrets
  existingSecretKey: TROVE_POSTGRES_URL
rootCredentials:
  existingSecret: trove-secrets
```

## PocketBase Integration

Trove pairs with [PocketBase](https://pocketbase.io) (and [pg-pocketbase](https://github.com/statewright/pg-pocketbase)) for file storage and backups.

Configure in PocketBase admin under Settings > S3:

| Setting | Value |
|---------|-------|
| Endpoint | `http://trove:9000` |
| Bucket | `pb-files` |
| Region | `us-east-1` |
| Access Key | your root access key |
| Secret Key | your root secret key |
| Force Path Style | enabled |

Or with pg-pocketbase's env-var auto-config (no admin UI needed):

```bash
PB_S3_ENABLED=true
PB_S3_ENDPOINT=http://trove:9000
PB_S3_BUCKET=pb-files
PB_S3_REGION=us-east-1
PB_S3_ACCESS_KEY=your-access-key
PB_S3_SECRET=your-secret-key
PB_S3_FORCE_PATH_STYLE=true
```

Create the bucket before first use:

```bash
aws --endpoint-url http://localhost:9000 s3 mb s3://pb-files
```

## PostgreSQL Schema

Trove creates these tables (auto-migrated on startup):

- `trove_buckets` — bucket names and creation timestamps
- `trove_objects` — object metadata (bucket, key, size, content type, ETag, blob hash)
- `trove_multipart_uploads` — in-progress multipart upload state
- `trove_multipart_parts` — completed parts of multipart uploads
- `trove_access_keys` — API access key/secret pairs
- `trove_bucket_permissions` — per-key, per-bucket read/write/owner grants

All tables are prefixed `trove_` to avoid conflicts when sharing a database.

## Development

```bash
# Run tests (unit tests, no PG required)
task test-unit

# Run all tests (requires PostgreSQL via PG_TEST_URL or localhost:5432)
task test

# Build binary
task build

# Docker
task docker-build
task docker-up
```

## License

MIT. See [LICENSE](LICENSE).
