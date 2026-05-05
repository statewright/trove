package server

import (
	"net/http"

	"github.com/statewright/trove/internal/metadata"
	"github.com/statewright/trove/internal/s3api"
	"github.com/statewright/trove/internal/storage"
)

// Server is the trove HTTP server.
type Server struct {
	handler *s3api.Handler
}

// New creates a new Server.
func New(store *metadata.Store, blobs *storage.BlobStore) *Server {
	return &Server{
		handler: s3api.NewHandler(store, blobs),
	}
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.handler)
}
