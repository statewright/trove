package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestDeriveSigningKey(t *testing.T) {
	// AWS test vector
	secretKey := "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
	date := "20150830"
	region := "us-east-1"
	service := "iam"

	key := deriveSigningKey(secretKey, date, region, service)
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}
}

func TestParseAuthHeader(t *testing.T) {
	header := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=abcdef1234567890"

	parts, err := parseAuthHeader(header)
	if err != nil {
		t.Fatalf("parseAuthHeader: %v", err)
	}

	if parts.AccessKeyID != "AKIDEXAMPLE" {
		t.Fatalf("expected access key AKIDEXAMPLE, got %s", parts.AccessKeyID)
	}
	if parts.Date != "20150830" {
		t.Fatalf("expected date 20150830, got %s", parts.Date)
	}
	if parts.Region != "us-east-1" {
		t.Fatalf("expected region us-east-1, got %s", parts.Region)
	}
	if parts.Service != "s3" {
		t.Fatalf("expected service s3, got %s", parts.Service)
	}
	if parts.Signature != "abcdef1234567890" {
		t.Fatalf("expected signature abcdef1234567890, got %s", parts.Signature)
	}
	if len(parts.SignedHeaders) != 3 {
		t.Fatalf("expected 3 signed headers, got %d", len(parts.SignedHeaders))
	}
}

func TestParseAuthHeader_Invalid(t *testing.T) {
	cases := []string{
		"",
		"Basic dXNlcjpwYXNz",
		"AWS4-HMAC-SHA256",
		"AWS4-HMAC-SHA256 Credential=bad",
	}
	for _, c := range cases {
		if _, err := parseAuthHeader(c); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}

func TestCanonicalRequest(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com/test/path?foo=bar&baz=qux", nil)
	req.Header.Set("Host", "example.com")
	req.Header.Set("X-Amz-Date", "20150830T123600Z")
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")

	signedHeaders := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	cr := canonicalRequest(req, signedHeaders)

	if cr == "" {
		t.Fatal("canonical request should not be empty")
	}

	// Should contain method, path, query, headers, signed header list, payload hash
	// Just verify it's well-formed (contains newlines separating components)
	lines := 0
	for _, c := range cr {
		if c == '\n' {
			lines++
		}
	}
	// canonical request has: method \n uri \n query \n headers \n \n signed_headers \n payload_hash
	// That's at least 6 newlines
	if lines < 5 {
		t.Fatalf("canonical request seems malformed, only %d newlines:\n%s", lines, cr)
	}
}

func TestVerifySignature_RoundTrip(t *testing.T) {
	// Sign a request, then verify it
	secretKey := "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
	accessKey := "AKIDEXAMPLE"
	region := "us-east-1"

	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	req.Host = "example.com"

	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	dateStr := now.Format("20060102")
	datetimeStr := now.Format("20060102T150405Z")

	req.Header.Set("X-Amz-Date", datetimeStr)
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	req.Header.Set("Host", "example.com")

	signedHeaders := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	signature := computeSignature(req, signedHeaders, secretKey, dateStr, region, "s3")

	// Build the auth header
	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential="+accessKey+"/"+dateStr+"/"+region+"/s3/aws4_request, "+
			"SignedHeaders=host;x-amz-content-sha256;x-amz-date, "+
			"Signature="+signature)

	// Verify
	parts, err := parseAuthHeader(req.Header.Get("Authorization"))
	if err != nil {
		t.Fatalf("parseAuthHeader: %v", err)
	}

	expectedSig := computeSignature(req, parts.SignedHeaders, secretKey, parts.Date, parts.Region, parts.Service)
	if expectedSig != parts.Signature {
		t.Fatalf("signature mismatch: computed %s, got %s", expectedSig, parts.Signature)
	}
}
