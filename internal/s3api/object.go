package s3api

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/statewright/trove/internal/metadata"
)

func (h *Handler) handlePutObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	// Verify bucket exists
	if err := h.store.HeadBucket(bucket); err != nil {
		if errors.Is(err, metadata.ErrBucketNotFound) {
			errNoSuchBucket(w, r)
			return
		}
		logError(r, "put object: head bucket", err)
		errInternalError(w, r, "internal error")
		return
	}

	// Compute MD5 while writing to blob store
	md5Hash := md5.New()
	tee := io.TeeReader(r.Body, md5Hash)

	blobHash, size, err := h.blobs.Put(tee)
	if err != nil {
		logError(r, "put object: store blob", err)
		errInternalError(w, r, "internal error")
		return
	}

	etag := fmt.Sprintf(`"%s"`, hex.EncodeToString(md5Hash.Sum(nil)))

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	obj := &metadata.Object{
		Bucket:      bucket,
		Key:         key,
		Size:        size,
		ContentType: contentType,
		ETag:        etag,
		BlobHash:    blobHash,
		Metadata:    extractMetadata(r),
	}

	prevHash, err := h.store.PutObject(obj)
	if err != nil {
		logError(r, "put object: save metadata", err)
		errInternalError(w, r, "internal error")
		return
	}

	// Clean up old blob if replaced and no longer referenced
	if prevHash != "" && prevHash != blobHash {
		h.maybeDeleteBlob(r, prevHash)
	}

	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleGetObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	obj, err := h.store.GetObject(bucket, key)
	if err != nil {
		if errors.Is(err, metadata.ErrObjectNotFound) {
			errNoSuchKey(w, r)
			return
		}
		logError(r, "get object", err)
		errInternalError(w, r, "internal error")
		return
	}

	rc, err := h.blobs.Get(obj.BlobHash)
	if err != nil {
		logError(r, "get object: read blob", err)
		errInternalError(w, r, "internal error")
		return
	}
	defer rc.Close()

	// Set response headers
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Last-Modified", obj.CreatedAt.UTC().Format(http.TimeFormat))
	w.Header().Set("Accept-Ranges", "bytes")

	for k, v := range obj.Metadata {
		w.Header().Set("X-Amz-Meta-"+k, v)
	}

	// Handle Range requests
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		h.serveRange(w, r, rc, obj, rangeHeader)
		return
	}

	w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	w.WriteHeader(http.StatusOK)
	io.Copy(w, rc)
}

func (h *Handler) handleHeadObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	obj, err := h.store.HeadObject(bucket, key)
	if err != nil {
		if errors.Is(err, metadata.ErrObjectNotFound) {
			errNoSuchKey(w, r)
			return
		}
		logError(r, "head object", err)
		errInternalError(w, r, "internal error")
		return
	}

	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Last-Modified", obj.CreatedAt.UTC().Format(http.TimeFormat))
	w.Header().Set("Accept-Ranges", "bytes")

	for k, v := range obj.Metadata {
		w.Header().Set("X-Amz-Meta-"+k, v)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleDeleteObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	blobHash, err := h.store.DeleteObject(bucket, key)
	if err != nil {
		logError(r, "delete object", err)
		errInternalError(w, r, "internal error")
		return
	}

	if blobHash != "" {
		h.maybeDeleteBlob(r, blobHash)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) serveRange(w http.ResponseWriter, r *http.Request, rc io.ReadCloser, obj *metadata.Object, rangeHeader string) {
	// Parse "bytes=start-end"
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		errInvalidRange(w, r)
		return
	}

	rangeSpec := rangeHeader[len("bytes="):]
	parts := strings.SplitN(rangeSpec, "-", 2)
	if len(parts) != 2 {
		errInvalidRange(w, r)
		return
	}

	var start, end int64
	totalSize := obj.Size

	if parts[0] == "" {
		// Suffix range: -N (last N bytes)
		suffixLen, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffixLen <= 0 {
			errInvalidRange(w, r)
			return
		}
		start = totalSize - suffixLen
		if start < 0 {
			start = 0
		}
		end = totalSize - 1
	} else {
		var err error
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || start < 0 {
			errInvalidRange(w, r)
			return
		}

		if parts[1] == "" {
			end = totalSize - 1
		} else {
			end, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil || end < start {
				errInvalidRange(w, r)
				return
			}
		}
	}

	if start >= totalSize {
		errInvalidRange(w, r)
		return
	}
	if end >= totalSize {
		end = totalSize - 1
	}

	// Seek to start position by discarding bytes
	if seeker, ok := rc.(io.Seeker); ok {
		if _, err := seeker.Seek(start, io.SeekStart); err != nil {
			errInternalError(w, r, "seek failed")
			return
		}
	} else {
		if _, err := io.CopyN(io.Discard, rc, start); err != nil {
			errInternalError(w, r, "seek failed")
			return
		}
	}

	contentLength := end - start + 1

	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize))
	w.WriteHeader(http.StatusPartialContent)

	io.CopyN(w, rc, contentLength)
}

func (h *Handler) maybeDeleteBlob(r *http.Request, blobHash string) {
	count, err := h.store.BlobHashRefCount(blobHash)
	if err != nil {
		logError(r, "ref count check", err)
		return
	}
	if count == 0 {
		h.blobs.Delete(blobHash)
	}
}

var _ = time.RFC3339 // import anchor
