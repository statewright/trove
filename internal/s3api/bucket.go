package s3api

import (
	"errors"
	"net/http"

	"github.com/statewright/trove/internal/metadata"
)

func (h *Handler) handleHeadBucket(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.store.HeadBucket(bucket); err != nil {
		if errors.Is(err, metadata.ErrBucketNotFound) {
			errNoSuchBucket(w, r)
			return
		}
		logError(r, "head bucket", err)
		errInternalError(w, r, "internal error")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleCreateBucket(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.store.CreateBucket(bucket); err != nil {
		// Check for duplicate bucket (unique constraint violation)
		if isDuplicateKeyError(err) {
			errBucketAlreadyExists(w, r)
			return
		}
		logError(r, "create bucket", err)
		errInternalError(w, r, "internal error")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleDeleteBucket(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.store.DeleteBucket(bucket); err != nil {
		if errors.Is(err, metadata.ErrBucketNotFound) {
			errNoSuchBucket(w, r)
			return
		}
		if errors.Is(err, metadata.ErrBucketNotEmpty) {
			errBucketNotEmpty(w, r)
			return
		}
		logError(r, "delete bucket", err)
		errInternalError(w, r, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListBuckets(w http.ResponseWriter, r *http.Request) {
	buckets, err := h.store.ListBuckets()
	if err != nil {
		logError(r, "list buckets", err)
		errInternalError(w, r, "internal error")
		return
	}

	result := ListBucketsResult{
		Xmlns: "http://s3.amazonaws.com/doc/2006-03-01/",
		Owner: Owner{ID: "trove", DisplayName: "trove"},
	}

	for _, b := range buckets {
		result.Buckets = append(result.Buckets, BucketEntry{
			Name:         b.Name,
			CreationDate: formatTime(b.CreatedAt),
		})
	}

	writeXML(w, http.StatusOK, result)
}

// isDuplicateKeyError checks for PostgreSQL unique constraint violation.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	// pgx wraps the PG error; check the error string for the SQLSTATE
	return contains(err.Error(), "23505") || contains(err.Error(), "duplicate key")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
