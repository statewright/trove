package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlobStore_PutAndGet(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	content := []byte("hello world")
	hash, size, err := bs.Put(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if size != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), size)
	}

	// Verify hash is correct SHA-256
	expected := sha256.Sum256(content)
	expectedHex := hex.EncodeToString(expected[:])
	if hash != expectedHex {
		t.Fatalf("expected hash %s, got %s", expectedHex, hash)
	}

	// Get it back
	rc, err := bs.Get(hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: got %q, want %q", got, content)
	}
}

func TestBlobStore_Deduplication(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	content := []byte("duplicate content")

	hash1, _, err := bs.Put(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put 1: %v", err)
	}

	hash2, _, err := bs.Put(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put 2: %v", err)
	}

	if hash1 != hash2 {
		t.Fatalf("same content should produce same hash: %s != %s", hash1, hash2)
	}

	// Should only be one file on disk
	count := countBlobFiles(t, dir)
	if count != 1 {
		t.Fatalf("expected 1 blob file, got %d", count)
	}
}

func TestBlobStore_Exists(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	if bs.Exists("nonexistent") {
		t.Fatal("should not exist")
	}

	hash, _, err := bs.Put(strings.NewReader("test"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if !bs.Exists(hash) {
		t.Fatal("should exist after Put")
	}
}

func TestBlobStore_Delete(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	hash, _, err := bs.Put(strings.NewReader("to be deleted"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := bs.Delete(hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if bs.Exists(hash) {
		t.Fatal("should not exist after Delete")
	}

	_, err = bs.Get(hash)
	if err == nil {
		t.Fatal("Get should fail after Delete")
	}
}

func TestBlobStore_DeleteNonexistent(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	// Deleting a nonexistent blob should not error
	if err := bs.Delete("nonexistent_hash"); err != nil {
		t.Fatalf("Delete nonexistent should not error: %v", err)
	}
}

func TestBlobStore_GetNonexistent(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	_, err = bs.Get("nonexistent_hash")
	if err == nil {
		t.Fatal("Get nonexistent should error")
	}
}

func TestBlobStore_LargeBlob(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	// 1MB of data
	content := make([]byte, 1<<20)
	for i := range content {
		content[i] = byte(i % 256)
	}

	hash, size, err := bs.Put(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if size != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), size)
	}

	rc, err := bs.Get(hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("large blob content mismatch")
	}
}

func TestBlobStore_EmptyBlob(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	hash, size, err := bs.Put(bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if size != 0 {
		t.Fatalf("expected size 0, got %d", size)
	}

	rc, err := bs.Get(hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("expected empty content, got %d bytes", len(got))
	}
}

func TestBlobStore_PathLayout(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	hash, _, err := bs.Put(strings.NewReader("path test"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Verify the file is stored at blobs/{h[0:2]}/{h[2:4]}/{h}
	expectedPath := filepath.Join(dir, "blobs", hash[0:2], hash[2:4], hash)
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("blob not at expected path %s: %v", expectedPath, err)
	}
}

func countBlobFiles(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	filepath.Walk(filepath.Join(dir, "blobs"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})
	return count
}
