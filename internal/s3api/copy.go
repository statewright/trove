package s3api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/statewright/trove/internal/metadata"
)

func (h *Handler) handleCopyObject(w http.ResponseWriter, r *http.Request, destBucket, destKey string) {
	// Parse x-amz-copy-source header
	copySource := r.Header.Get("X-Amz-Copy-Source")
	copySource, _ = url.PathUnescape(copySource)
	copySource = strings.TrimPrefix(copySource, "/")

	srcBucket, srcKey := splitCopySource(copySource)
	if srcBucket == "" || srcKey == "" {
		errInvalidRequest(w, r, "invalid x-amz-copy-source")
		return
	}

	// Get source object
	srcObj, err := h.store.GetObject(srcBucket, srcKey)
	if err != nil {
		if errors.Is(err, metadata.ErrObjectNotFound) {
			errNoSuchKey(w, r)
			return
		}
		logError(r, "copy object: get source", err)
		errInternalError(w, r, "internal error")
		return
	}

	// Verify destination bucket exists
	if err := h.store.HeadBucket(destBucket); err != nil {
		if errors.Is(err, metadata.ErrBucketNotFound) {
			errNoSuchBucket(w, r)
			return
		}
		logError(r, "copy object: head dest bucket", err)
		errInternalError(w, r, "internal error")
		return
	}

	// Create destination object with same blob hash (no data copy needed)
	destObj := &metadata.Object{
		Bucket:      destBucket,
		Key:         destKey,
		Size:        srcObj.Size,
		ContentType: srcObj.ContentType,
		ETag:        srcObj.ETag,
		BlobHash:    srcObj.BlobHash,
		Metadata:    srcObj.Metadata,
	}

	prevHash, err := h.store.PutObject(destObj)
	if err != nil {
		logError(r, "copy object: put dest", err)
		errInternalError(w, r, "internal error")
		return
	}

	if prevHash != "" && prevHash != srcObj.BlobHash {
		h.maybeDeleteBlob(r, prevHash)
	}

	result := CopyObjectResult{
		ETag:         srcObj.ETag,
		LastModified: formatTime(srcObj.CreatedAt),
	}

	writeXML(w, http.StatusOK, result)
}

func splitCopySource(source string) (bucket, key string) {
	idx := strings.IndexByte(source, '/')
	if idx < 0 {
		return source, ""
	}
	return source[:idx], source[idx+1:]
}
