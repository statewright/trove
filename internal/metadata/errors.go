package metadata

import "errors"

var (
	ErrBucketNotFound = errors.New("bucket not found")
	ErrBucketNotEmpty = errors.New("bucket not empty")
	ErrObjectNotFound = errors.New("object not found")
	ErrUploadNotFound = errors.New("upload not found")
)
