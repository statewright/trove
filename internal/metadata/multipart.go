package metadata

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// MultipartUpload represents an in-progress multipart upload.
type MultipartUpload struct {
	UploadID string
	Bucket   string
	Key      string
	Metadata map[string]string
}

// MultipartPart represents a completed part of a multipart upload.
type MultipartPart struct {
	PartNumber int
	Size       int64
	ETag       string
	BlobHash   string
}

// CreateMultipartUpload starts a new multipart upload and returns the upload ID.
func (s *Store) CreateMultipartUpload(bucket, key string, meta map[string]string) (string, error) {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}

	var uploadID string
	err = s.db.QueryRow(`
		INSERT INTO trove_multipart_uploads (bucket, key, metadata)
		VALUES ($1, $2, $3)
		RETURNING upload_id
	`, bucket, key, metaJSON).Scan(&uploadID)

	return uploadID, err
}

// PutMultipartPart records a completed part.
func (s *Store) PutMultipartPart(uploadID string, partNumber int, size int64, etag, blobHash string) error {
	_, err := s.db.Exec(`
		INSERT INTO trove_multipart_parts (upload_id, part_number, size, etag, blob_hash)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (upload_id, part_number) DO UPDATE SET
			size = EXCLUDED.size,
			etag = EXCLUDED.etag,
			blob_hash = EXCLUDED.blob_hash
	`, uploadID, partNumber, size, etag, blobHash)
	return err
}

// GetMultipartUpload retrieves a multipart upload by ID.
func (s *Store) GetMultipartUpload(uploadID string) (*MultipartUpload, error) {
	up := &MultipartUpload{UploadID: uploadID}
	var metaJSON []byte

	err := s.db.QueryRow(`
		SELECT bucket, key, metadata FROM trove_multipart_uploads WHERE upload_id = $1
	`, uploadID).Scan(&up.Bucket, &up.Key, &metaJSON)

	if err == sql.ErrNoRows {
		return nil, ErrUploadNotFound
	}
	if err != nil {
		return nil, err
	}

	if len(metaJSON) > 0 {
		json.Unmarshal(metaJSON, &up.Metadata)
	}

	return up, nil
}

// ListMultipartParts returns all parts for an upload, ordered by part number.
func (s *Store) ListMultipartParts(uploadID string) ([]MultipartPart, error) {
	rows, err := s.db.Query(`
		SELECT part_number, size, etag, blob_hash
		FROM trove_multipart_parts
		WHERE upload_id = $1
		ORDER BY part_number
	`, uploadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var parts []MultipartPart
	for rows.Next() {
		var p MultipartPart
		if err := rows.Scan(&p.PartNumber, &p.Size, &p.ETag, &p.BlobHash); err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}

	return parts, rows.Err()
}

// DeleteMultipartUpload removes a multipart upload and all its parts.
// Returns the blob hashes of all parts for cleanup.
func (s *Store) DeleteMultipartUpload(uploadID string) ([]string, error) {
	// Get part blob hashes first
	rows, err := s.db.Query(
		`SELECT blob_hash FROM trove_multipart_parts WHERE upload_id = $1`,
		uploadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		hashes = append(hashes, h)
	}

	// Delete the upload (cascades to parts)
	result, err := s.db.Exec(`DELETE FROM trove_multipart_uploads WHERE upload_id = $1`, uploadID)
	if err != nil {
		return nil, err
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		return nil, ErrUploadNotFound
	}

	return hashes, nil
}
