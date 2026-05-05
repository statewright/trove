package metadata

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Object represents an S3 object's metadata.
type Object struct {
	Bucket      string
	Key         string
	Size        int64
	ContentType string
	ETag        string
	BlobHash    string
	Metadata    map[string]string
	CreatedAt   time.Time
}

// PutObject creates or replaces an object's metadata.
// Returns the previous blob hash if the object was replaced (for cleanup), or "" if new.
func (s *Store) PutObject(obj *Object) (previousBlobHash string, err error) {
	metaJSON, err := json.Marshal(obj.Metadata)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}

	// Try to get existing blob hash for potential cleanup
	_ = s.db.QueryRow(
		`SELECT blob_hash FROM trove_objects WHERE bucket = $1 AND key = $2`,
		obj.Bucket, obj.Key,
	).Scan(&previousBlobHash)

	_, err = s.db.Exec(`
		INSERT INTO trove_objects (bucket, key, size, content_type, etag, blob_hash, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (bucket, key) DO UPDATE SET
			size = EXCLUDED.size,
			content_type = EXCLUDED.content_type,
			etag = EXCLUDED.etag,
			blob_hash = EXCLUDED.blob_hash,
			metadata = EXCLUDED.metadata,
			created_at = NOW()
	`, obj.Bucket, obj.Key, obj.Size, obj.ContentType, obj.ETag, obj.BlobHash, metaJSON)

	return previousBlobHash, err
}

// GetObject retrieves an object's metadata.
func (s *Store) GetObject(bucket, key string) (*Object, error) {
	obj := &Object{Bucket: bucket, Key: key}
	var metaJSON []byte

	err := s.db.QueryRow(`
		SELECT size, content_type, etag, blob_hash, metadata, created_at
		FROM trove_objects WHERE bucket = $1 AND key = $2
	`, bucket, key).Scan(&obj.Size, &obj.ContentType, &obj.ETag, &obj.BlobHash, &metaJSON, &obj.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, ErrObjectNotFound
	}
	if err != nil {
		return nil, err
	}

	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &obj.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}

	return obj, nil
}

// DeleteObject removes an object's metadata.
// Returns the blob hash of the deleted object for cleanup, or "" if not found.
func (s *Store) DeleteObject(bucket, key string) (blobHash string, err error) {
	err = s.db.QueryRow(
		`DELETE FROM trove_objects WHERE bucket = $1 AND key = $2 RETURNING blob_hash`,
		bucket, key,
	).Scan(&blobHash)

	if err == sql.ErrNoRows {
		return "", nil // not an error to delete nonexistent
	}
	return blobHash, err
}

// HeadObject checks if an object exists and returns its metadata.
func (s *Store) HeadObject(bucket, key string) (*Object, error) {
	return s.GetObject(bucket, key)
}

// ListObjectsV2Result holds the result of a ListObjectsV2 call.
type ListObjectsV2Result struct {
	Objects               []Object
	CommonPrefixes        []string
	NextContinuationToken string
	IsTruncated           bool
}

// ListObjectsV2 lists objects with prefix/delimiter/pagination support.
func (s *Store) ListObjectsV2(bucket, prefix, delimiter, startAfter, continuationToken string, maxKeys int) (*ListObjectsV2Result, error) {
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	// Determine the effective start-after key
	effectiveStartAfter := startAfter
	if continuationToken != "" {
		effectiveStartAfter = continuationToken
	}

	result := &ListObjectsV2Result{}

	if delimiter == "" {
		// No delimiter: flat listing
		return s.listFlat(bucket, prefix, effectiveStartAfter, maxKeys)
	}

	// With delimiter: need to compute common prefixes
	return s.listWithDelimiter(bucket, prefix, delimiter, effectiveStartAfter, maxKeys, result)
}

func (s *Store) listFlat(bucket, prefix, startAfter string, maxKeys int) (*ListObjectsV2Result, error) {
	rows, err := s.db.Query(`
		SELECT key, size, content_type, etag, blob_hash, metadata, created_at
		FROM trove_objects
		WHERE bucket = $1
			AND key LIKE $2
			AND ($3 = '' OR key > $3)
		ORDER BY key
		LIMIT $4
	`, bucket, prefix+"%", startAfter, maxKeys+1) // fetch one extra to detect truncation

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := &ListObjectsV2Result{}
	for rows.Next() {
		var obj Object
		var metaJSON []byte
		obj.Bucket = bucket
		if err := rows.Scan(&obj.Key, &obj.Size, &obj.ContentType, &obj.ETag, &obj.BlobHash, &metaJSON, &obj.CreatedAt); err != nil {
			return nil, err
		}
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &obj.Metadata)
		}
		result.Objects = append(result.Objects, obj)
	}

	if len(result.Objects) > maxKeys {
		result.IsTruncated = true
		result.NextContinuationToken = result.Objects[maxKeys-1].Key
		result.Objects = result.Objects[:maxKeys]
	}

	return result, rows.Err()
}

func (s *Store) listWithDelimiter(bucket, prefix, delimiter, startAfter string, maxKeys int, result *ListObjectsV2Result) (*ListObjectsV2Result, error) {
	// Fetch all matching keys and compute prefixes in Go
	// This is simpler and correct; PG-side grouping with variable delimiters is complex
	rows, err := s.db.Query(`
		SELECT key, size, content_type, etag, blob_hash, metadata, created_at
		FROM trove_objects
		WHERE bucket = $1
			AND key LIKE $2
			AND ($3 = '' OR key > $3)
		ORDER BY key
	`, bucket, prefix+"%", startAfter)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prefixLen := len(prefix)
	seenPrefixes := map[string]bool{}
	count := 0

	for rows.Next() {
		if count >= maxKeys {
			result.IsTruncated = true
			break
		}

		var obj Object
		var metaJSON []byte
		obj.Bucket = bucket
		if err := rows.Scan(&obj.Key, &obj.Size, &obj.ContentType, &obj.ETag, &obj.BlobHash, &metaJSON, &obj.CreatedAt); err != nil {
			return nil, err
		}
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &obj.Metadata)
		}

		// Check if the key after the prefix contains the delimiter
		rest := obj.Key[prefixLen:]
		delimIdx := indexOf(rest, delimiter)

		if delimIdx >= 0 {
			// This is a "directory" — add common prefix
			commonPrefix := prefix + rest[:delimIdx+len(delimiter)]
			if !seenPrefixes[commonPrefix] {
				seenPrefixes[commonPrefix] = true
				result.CommonPrefixes = append(result.CommonPrefixes, commonPrefix)
				count++
			}
		} else {
			// This is a direct object
			result.Objects = append(result.Objects, obj)
			count++
		}
	}

	if result.IsTruncated && count > 0 {
		// Use the last key as continuation token
		if len(result.Objects) > 0 {
			result.NextContinuationToken = result.Objects[len(result.Objects)-1].Key
		} else if len(result.CommonPrefixes) > 0 {
			result.NextContinuationToken = result.CommonPrefixes[len(result.CommonPrefixes)-1]
		}
	}

	return result, rows.Err()
}

// BlobHashRefCount returns the number of objects referencing a given blob hash.
func (s *Store) BlobHashRefCount(blobHash string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM trove_objects WHERE blob_hash = $1`,
		blobHash,
	).Scan(&count)
	return count, err
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
