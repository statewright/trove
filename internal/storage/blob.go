package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// BlobStore provides content-addressed blob storage on the local filesystem.
// Blobs are stored at {root}/blobs/{hash[0:2]}/{hash[2:4]}/{hash}.
type BlobStore struct {
	root string
}

// NewBlobStore creates a BlobStore rooted at the given directory.
// Creates the blobs subdirectory if it doesn't exist.
func NewBlobStore(root string) (*BlobStore, error) {
	blobDir := filepath.Join(root, "blobs")
	if err := os.MkdirAll(blobDir, 0755); err != nil {
		return nil, fmt.Errorf("create blob directory: %w", err)
	}
	return &BlobStore{root: root}, nil
}

// Put reads all data from r, stores it content-addressed, and returns the
// SHA-256 hash and size. If a blob with the same hash already exists, the
// write is skipped (deduplication).
func (s *BlobStore) Put(r io.Reader) (hash string, size int64, err error) {
	// Write to a temp file while computing hash
	tmpDir := filepath.Join(s.root, "blobs", "tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", 0, fmt.Errorf("create temp dir: %w", err)
	}

	tmp, err := os.CreateTemp(tmpDir, "blob-*")
	if err != nil {
		return "", 0, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()

	h := sha256.New()
	w := io.MultiWriter(tmp, h)

	size, err = io.Copy(w, r)
	if err != nil {
		return "", 0, fmt.Errorf("write blob: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		return "", 0, fmt.Errorf("fsync blob: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", 0, fmt.Errorf("close temp file: %w", err)
	}

	hash = hex.EncodeToString(h.Sum(nil))
	finalPath := s.blobPath(hash)

	// Deduplication: if the blob already exists, discard the temp file
	if _, statErr := os.Stat(finalPath); statErr == nil {
		os.Remove(tmpPath)
		return hash, size, nil
	}

	// Ensure parent directories exist
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return "", 0, fmt.Errorf("create blob parent dir: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", 0, fmt.Errorf("rename blob: %w", err)
	}

	return hash, size, nil
}

// Get returns a ReadCloser for the blob with the given hash.
// Returns an error if the blob does not exist.
func (s *BlobStore) Get(hash string) (io.ReadCloser, error) {
	f, err := os.Open(s.blobPath(hash))
	if err != nil {
		return nil, fmt.Errorf("open blob %s: %w", hash, err)
	}
	return f, nil
}

// Exists returns true if a blob with the given hash exists.
func (s *BlobStore) Exists(hash string) bool {
	_, err := os.Stat(s.blobPath(hash))
	return err == nil
}

// Delete removes the blob with the given hash.
// No error if the blob does not exist.
func (s *BlobStore) Delete(hash string) error {
	err := os.Remove(s.blobPath(hash))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *BlobStore) blobPath(hash string) string {
	if len(hash) < 4 {
		return filepath.Join(s.root, "blobs", hash)
	}
	return filepath.Join(s.root, "blobs", hash[0:2], hash[2:4], hash)
}
