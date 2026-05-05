package s3api

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"

	"github.com/statewright/trove/internal/metadata"
)

func (h *Handler) handleCreateMultipartUpload(w http.ResponseWriter, r *http.Request, bucket, key string) {
	if err := h.store.HeadBucket(bucket); err != nil {
		if errors.Is(err, metadata.ErrBucketNotFound) {
			errNoSuchBucket(w, r)
			return
		}
		logError(r, "create multipart: head bucket", err)
		errInternalError(w, r, "internal error")
		return
	}

	meta := extractMetadata(r)

	uploadID, err := h.store.CreateMultipartUpload(bucket, key, meta)
	if err != nil {
		logError(r, "create multipart", err)
		errInternalError(w, r, "internal error")
		return
	}

	result := InitiateMultipartUploadResult{
		Xmlns:    "http://s3.amazonaws.com/doc/2006-03-01/",
		Bucket:   bucket,
		Key:      key,
		UploadId: uploadID,
	}

	writeXML(w, http.StatusOK, result)
}

func (h *Handler) handleUploadPart(w http.ResponseWriter, r *http.Request, bucket, key string) {
	uploadID := r.URL.Query().Get("uploadId")
	partNumberStr := r.URL.Query().Get("partNumber")

	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil || partNumber < 1 {
		errInvalidRequest(w, r, "invalid partNumber")
		return
	}

	// Verify upload exists
	_, err = h.store.GetMultipartUpload(uploadID)
	if err != nil {
		if errors.Is(err, metadata.ErrUploadNotFound) {
			errNoSuchUpload(w, r)
			return
		}
		logError(r, "upload part: get upload", err)
		errInternalError(w, r, "internal error")
		return
	}

	// Store the part blob
	md5Hash := md5.New()
	tee := io.TeeReader(r.Body, md5Hash)

	blobHash, size, err := h.blobs.Put(tee)
	if err != nil {
		logError(r, "upload part: store blob", err)
		errInternalError(w, r, "internal error")
		return
	}

	etag := fmt.Sprintf(`"%s"`, hex.EncodeToString(md5Hash.Sum(nil)))

	if err := h.store.PutMultipartPart(uploadID, partNumber, size, etag, blobHash); err != nil {
		logError(r, "upload part: save", err)
		errInternalError(w, r, "internal error")
		return
	}

	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleCompleteMultipartUpload(w http.ResponseWriter, r *http.Request, bucket, key string) {
	uploadID := r.URL.Query().Get("uploadId")

	// Parse request body
	var req CompleteMultipartUploadRequest
	if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
		errInvalidRequest(w, r, "invalid request body")
		return
	}

	// Get upload metadata
	upload, err := h.store.GetMultipartUpload(uploadID)
	if err != nil {
		if errors.Is(err, metadata.ErrUploadNotFound) {
			errNoSuchUpload(w, r)
			return
		}
		logError(r, "complete multipart: get upload", err)
		errInternalError(w, r, "internal error")
		return
	}

	// Get all parts
	parts, err := h.store.ListMultipartParts(uploadID)
	if err != nil {
		logError(r, "complete multipart: list parts", err)
		errInternalError(w, r, "internal error")
		return
	}

	// Sort requested parts by part number
	sort.Slice(req.Parts, func(i, j int) bool {
		return req.Parts[i].PartNumber < req.Parts[j].PartNumber
	})

	// Build part hash map for lookup
	partMap := make(map[int]metadata.MultipartPart)
	for _, p := range parts {
		partMap[p.PartNumber] = p
	}

	// Concatenate parts into final blob
	var readers []io.Reader
	var totalSize int64
	var partBlobHashes []string

	for _, rp := range req.Parts {
		part, ok := partMap[rp.PartNumber]
		if !ok {
			errInvalidRequest(w, r, fmt.Sprintf("part %d not found", rp.PartNumber))
			return
		}

		rc, err := h.blobs.Get(part.BlobHash)
		if err != nil {
			logError(r, "complete multipart: get part blob", err)
			errInternalError(w, r, "internal error")
			return
		}
		defer rc.Close()

		readers = append(readers, rc)
		totalSize += part.Size
		partBlobHashes = append(partBlobHashes, part.BlobHash)
	}

	// Store concatenated blob
	md5Hash := md5.New()
	combined := io.TeeReader(io.MultiReader(readers...), md5Hash)

	blobHash, size, err := h.blobs.Put(combined)
	if err != nil {
		logError(r, "complete multipart: store combined blob", err)
		errInternalError(w, r, "internal error")
		return
	}

	// S3 multipart ETag format: md5-partcount
	etag := fmt.Sprintf(`"%s-%d"`, hex.EncodeToString(md5Hash.Sum(nil)), len(req.Parts))

	contentType := "application/octet-stream"
	if ct, ok := upload.Metadata["content-type"]; ok {
		contentType = ct
		delete(upload.Metadata, "content-type")
	}

	obj := &metadata.Object{
		Bucket:      bucket,
		Key:         key,
		Size:        size,
		ContentType: contentType,
		ETag:        etag,
		BlobHash:    blobHash,
		Metadata:    upload.Metadata,
	}

	prevHash, err := h.store.PutObject(obj)
	if err != nil {
		logError(r, "complete multipart: put object", err)
		errInternalError(w, r, "internal error")
		return
	}

	// Clean up: delete multipart upload record and part blobs
	h.store.DeleteMultipartUpload(uploadID)
	for _, ph := range partBlobHashes {
		if ph != blobHash {
			h.maybeDeleteBlob(r, ph)
		}
	}
	if prevHash != "" && prevHash != blobHash {
		h.maybeDeleteBlob(r, prevHash)
	}

	result := CompleteMultipartUploadResult{
		Xmlns:  "http://s3.amazonaws.com/doc/2006-03-01/",
		Bucket: bucket,
		Key:    key,
		ETag:   etag,
	}

	writeXML(w, http.StatusOK, result)
}

func (h *Handler) handleAbortMultipartUpload(w http.ResponseWriter, r *http.Request, bucket, key string) {
	uploadID := r.URL.Query().Get("uploadId")

	hashes, err := h.store.DeleteMultipartUpload(uploadID)
	if err != nil {
		if errors.Is(err, metadata.ErrUploadNotFound) {
			errNoSuchUpload(w, r)
			return
		}
		logError(r, "abort multipart", err)
		errInternalError(w, r, "internal error")
		return
	}

	// Clean up part blobs
	for _, hash := range hashes {
		h.maybeDeleteBlob(r, hash)
	}

	w.WriteHeader(http.StatusNoContent)
}
