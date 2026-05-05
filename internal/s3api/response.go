package s3api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"time"
)

// ListBucketsResult is the XML response for ListBuckets.
type ListBucketsResult struct {
	XMLName xml.Name       `xml:"ListAllMyBucketsResult"`
	Xmlns   string         `xml:"xmlns,attr"`
	Owner   Owner          `xml:"Owner"`
	Buckets []BucketEntry  `xml:"Buckets>Bucket"`
}

// Owner represents the bucket owner.
type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

// BucketEntry represents a bucket in the listing.
type BucketEntry struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

// ListBucketResult is the XML response for ListObjectsV2.
type ListBucketResult struct {
	XMLName               xml.Name        `xml:"ListBucketResult"`
	Xmlns                 string          `xml:"xmlns,attr"`
	Name                  string          `xml:"Name"`
	Prefix                string          `xml:"Prefix"`
	Delimiter             string          `xml:"Delimiter,omitempty"`
	MaxKeys               int             `xml:"MaxKeys"`
	IsTruncated           bool            `xml:"IsTruncated"`
	KeyCount              int             `xml:"KeyCount"`
	ContinuationToken     string          `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string          `xml:"NextContinuationToken,omitempty"`
	StartAfter            string          `xml:"StartAfter,omitempty"`
	Contents              []ObjectEntry   `xml:"Contents"`
	CommonPrefixes        []CommonPrefix  `xml:"CommonPrefixes"`
}

// ObjectEntry represents an object in a listing.
type ObjectEntry struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

// CommonPrefix represents a grouped prefix in a listing.
type CommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

// CopyObjectResult is the XML response for CopyObject.
type CopyObjectResult struct {
	XMLName      xml.Name `xml:"CopyObjectResult"`
	ETag         string   `xml:"ETag"`
	LastModified string   `xml:"LastModified"`
}

// InitiateMultipartUploadResult is the XML response for CreateMultipartUpload.
type InitiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Xmlns    string   `xml:"xmlns,attr"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadId string   `xml:"UploadId"`
}

// CompleteMultipartUploadResult is the XML response for CompleteMultipartUpload.
type CompleteMultipartUploadResult struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
	Xmlns    string   `xml:"xmlns,attr"`
	Location string   `xml:"Location"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	ETag     string   `xml:"ETag"`
}

// CompleteMultipartUploadRequest is the XML request body for CompleteMultipartUpload.
type CompleteMultipartUploadRequest struct {
	Parts []CompletedPart `xml:"Part"`
}

// CompletedPart is a part in a CompleteMultipartUpload request.
type CompletedPart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

func writeXML(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(statusCode)
	xml.NewEncoder(w).Encode(v)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func requestID(r *http.Request) string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
