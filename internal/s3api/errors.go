package s3api

import (
	"encoding/xml"
	"net/http"
)

// S3Error represents an S3 API error response.
type S3Error struct {
	XMLName    xml.Name `xml:"Error"`
	Code       string   `xml:"Code"`
	Message    string   `xml:"Message"`
	Resource   string   `xml:"Resource,omitempty"`
	RequestID  string   `xml:"RequestId"`
	StatusCode int      `xml:"-"`
}

func writeError(w http.ResponseWriter, r *http.Request, s3err S3Error) {
	s3err.RequestID = requestID(r)
	s3err.Resource = r.URL.Path

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(s3err.StatusCode)
	xml.NewEncoder(w).Encode(s3err)
}

func errNoSuchBucket(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, S3Error{
		Code:       "NoSuchBucket",
		Message:    "The specified bucket does not exist",
		StatusCode: http.StatusNotFound,
	})
}

func errNoSuchKey(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, S3Error{
		Code:       "NoSuchKey",
		Message:    "The specified key does not exist",
		StatusCode: http.StatusNotFound,
	})
}

func errBucketAlreadyExists(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, S3Error{
		Code:       "BucketAlreadyExists",
		Message:    "The requested bucket name is not available",
		StatusCode: http.StatusConflict,
	})
}

func errBucketNotEmpty(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, S3Error{
		Code:       "BucketNotEmpty",
		Message:    "The bucket you tried to delete is not empty",
		StatusCode: http.StatusConflict,
	})
}

func errAccessDenied(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, S3Error{
		Code:       "AccessDenied",
		Message:    "Access Denied",
		StatusCode: http.StatusForbidden,
	})
}

func errInvalidRequest(w http.ResponseWriter, r *http.Request, msg string) {
	writeError(w, r, S3Error{
		Code:       "InvalidRequest",
		Message:    msg,
		StatusCode: http.StatusBadRequest,
	})
}

func errInternalError(w http.ResponseWriter, r *http.Request, msg string) {
	writeError(w, r, S3Error{
		Code:       "InternalError",
		Message:    msg,
		StatusCode: http.StatusInternalServerError,
	})
}

func errNoSuchUpload(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, S3Error{
		Code:       "NoSuchUpload",
		Message:    "The specified upload does not exist",
		StatusCode: http.StatusNotFound,
	})
}

func errInvalidRange(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, S3Error{
		Code:       "InvalidRange",
		Message:    "The requested range is not satisfiable",
		StatusCode: http.StatusRequestedRangeNotSatisfiable,
	})
}
