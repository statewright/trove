package s3api

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/statewright/trove/internal/auth"
	"github.com/statewright/trove/internal/metadata"
	"github.com/statewright/trove/internal/storage"
)

type contextKey string

const ctxAccessKeyID contextKey = "accessKeyID"

// Handler is the S3 API request handler.
type Handler struct {
	store *metadata.Store
	blobs *storage.BlobStore
}

// NewHandler creates a new S3 API handler.
func NewHandler(store *metadata.Store, blobs *storage.BlobStore) *Handler {
	return &Handler{store: store, blobs: blobs}
}

// ServeHTTP routes S3 API requests to the appropriate handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Auth middleware
	accessKeyID, err := auth.VerifyRequest(r, func(akid string) (string, error) {
		key, err := h.store.GetAccessKey(akid)
		if err != nil {
			return "", err
		}
		if key == nil {
			return "", &authError{msg: "unknown access key"}
		}
		return key.SecretKey, nil
	})
	if err != nil {
		errAccessDenied(w, r)
		return
	}

	// Inject access key into context
	ctx := context.WithValue(r.Context(), ctxAccessKeyID, accessKeyID)
	r = r.WithContext(ctx)

	// Parse bucket and key from path
	bucket, key := parsePath(r)

	// Route based on method + path + query
	switch {
	// Service-level operations
	case bucket == "" && r.Method == http.MethodGet:
		h.handleListBuckets(w, r)

	// Bucket-level operations
	case key == "":
		switch r.Method {
		case http.MethodPut:
			h.handleCreateBucket(w, r, bucket)
		case http.MethodDelete:
			h.handleDeleteBucket(w, r, bucket)
		case http.MethodHead:
			h.handleHeadBucket(w, r, bucket)
		case http.MethodGet:
			// ListObjectsV2
			h.handleListObjectsV2(w, r, bucket)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	// Object-level operations
	default:
		switch r.Method {
		case http.MethodPut:
			if r.URL.Query().Has("uploadId") && r.URL.Query().Has("partNumber") {
				h.handleUploadPart(w, r, bucket, key)
			} else if r.Header.Get("X-Amz-Copy-Source") != "" {
				h.handleCopyObject(w, r, bucket, key)
			} else {
				h.handlePutObject(w, r, bucket, key)
			}
		case http.MethodGet:
			h.handleGetObject(w, r, bucket, key)
		case http.MethodHead:
			h.handleHeadObject(w, r, bucket, key)
		case http.MethodDelete:
			if r.URL.Query().Has("uploadId") {
				h.handleAbortMultipartUpload(w, r, bucket, key)
			} else {
				h.handleDeleteObject(w, r, bucket, key)
			}
		case http.MethodPost:
			if r.URL.Query().Has("uploads") {
				h.handleCreateMultipartUpload(w, r, bucket, key)
			} else if r.URL.Query().Has("uploadId") {
				h.handleCompleteMultipartUpload(w, r, bucket, key)
			} else {
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

// parsePath extracts bucket and key from the request path.
// Supports path-style: /{bucket}/{key...}
func parsePath(r *http.Request) (bucket, key string) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		return "", ""
	}

	idx := strings.IndexByte(path, '/')
	if idx < 0 {
		return path, ""
	}

	return path[:idx], path[idx+1:]
}

// extractMetadata extracts x-amz-meta-* headers from the request.
func extractMetadata(r *http.Request) map[string]string {
	meta := make(map[string]string)
	for key, values := range r.Header {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-amz-meta-") {
			metaKey := lower[len("x-amz-meta-"):]
			if len(values) > 0 {
				meta[metaKey] = values[0]
			}
		}
	}
	return meta
}

type authError struct {
	msg string
}

func (e *authError) Error() string {
	return e.msg
}

func logError(r *http.Request, msg string, err error) {
	log.Printf("s3api: %s %s %s: %s: %v", r.Method, r.URL.Path, r.URL.RawQuery, msg, err)
}
