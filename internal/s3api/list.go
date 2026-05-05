package s3api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/statewright/trove/internal/metadata"
)

func (h *Handler) handleListObjectsV2(w http.ResponseWriter, r *http.Request, bucket string) {
	// Verify bucket exists
	if err := h.store.HeadBucket(bucket); err != nil {
		if errors.Is(err, metadata.ErrBucketNotFound) {
			errNoSuchBucket(w, r)
			return
		}
		logError(r, "list objects: head bucket", err)
		errInternalError(w, r, "internal error")
		return
	}

	q := r.URL.Query()
	prefix := q.Get("prefix")
	delimiter := q.Get("delimiter")
	startAfter := q.Get("start-after")
	continuationToken := q.Get("continuation-token")

	maxKeys := 1000
	if mk := q.Get("max-keys"); mk != "" {
		if parsed, err := strconv.Atoi(mk); err == nil && parsed > 0 {
			maxKeys = parsed
			if maxKeys > 1000 {
				maxKeys = 1000
			}
		}
	}

	result, err := h.store.ListObjectsV2(bucket, prefix, delimiter, startAfter, continuationToken, maxKeys)
	if err != nil {
		logError(r, "list objects", err)
		errInternalError(w, r, "internal error")
		return
	}

	xmlResult := ListBucketResult{
		Xmlns:                 "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:                  bucket,
		Prefix:                prefix,
		Delimiter:             delimiter,
		MaxKeys:               maxKeys,
		IsTruncated:           result.IsTruncated,
		ContinuationToken:     continuationToken,
		NextContinuationToken: result.NextContinuationToken,
		StartAfter:            startAfter,
	}

	for _, obj := range result.Objects {
		xmlResult.Contents = append(xmlResult.Contents, ObjectEntry{
			Key:          obj.Key,
			LastModified: formatTime(obj.CreatedAt),
			ETag:         obj.ETag,
			Size:         obj.Size,
			StorageClass: "STANDARD",
		})
	}

	for _, p := range result.CommonPrefixes {
		xmlResult.CommonPrefixes = append(xmlResult.CommonPrefixes, CommonPrefix{Prefix: p})
	}

	xmlResult.KeyCount = len(xmlResult.Contents) + len(xmlResult.CommonPrefixes)

	writeXML(w, http.StatusOK, xmlResult)
}
