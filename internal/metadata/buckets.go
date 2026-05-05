package metadata

import (
	"database/sql"
	"time"
)

// Bucket represents an S3 bucket.
type Bucket struct {
	Name      string
	CreatedAt time.Time
}

// CreateBucket creates a new bucket.
func (s *Store) CreateBucket(name string) error {
	_, err := s.db.Exec(
		`INSERT INTO trove_buckets (name) VALUES ($1)`,
		name,
	)
	return err
}

// DeleteBucket deletes a bucket. Fails if it contains objects.
func (s *Store) DeleteBucket(name string) error {
	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM trove_objects WHERE bucket = $1 LIMIT 1`,
		name,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrBucketNotEmpty
	}

	result, err := s.db.Exec(`DELETE FROM trove_buckets WHERE name = $1`, name)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrBucketNotFound
	}
	return nil
}

// HeadBucket checks if a bucket exists.
func (s *Store) HeadBucket(name string) error {
	var exists bool
	err := s.db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM trove_buckets WHERE name = $1)`,
		name,
	).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrBucketNotFound
	}
	return nil
}

// ListBuckets returns all buckets.
func (s *Store) ListBuckets() ([]Bucket, error) {
	rows, err := s.db.Query(`SELECT name, created_at FROM trove_buckets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Name, &b.CreatedAt); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

// BucketExists returns true if the bucket exists.
func (s *Store) BucketExists(name string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM trove_buckets WHERE name = $1)`,
		name,
	).Scan(&exists)
	return exists, err
}

var _ = sql.ErrNoRows // import anchor
