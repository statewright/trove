package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// AuthParts holds the parsed components of an AWS SigV4 Authorization header.
type AuthParts struct {
	AccessKeyID   string
	Date          string
	Region        string
	Service       string
	SignedHeaders []string
	Signature     string
}

// VerifyRequest verifies the AWS SigV4 signature on an HTTP request.
// Returns the access key ID if valid, or an error if invalid.
func VerifyRequest(r *http.Request, secretKeyLookup func(accessKeyID string) (string, error)) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("missing Authorization header")
	}

	parts, err := parseAuthHeader(authHeader)
	if err != nil {
		return "", fmt.Errorf("invalid Authorization header: %w", err)
	}

	secretKey, err := secretKeyLookup(parts.AccessKeyID)
	if err != nil {
		return "", fmt.Errorf("access key lookup: %w", err)
	}

	expectedSig := computeSignature(r, parts.SignedHeaders, secretKey, parts.Date, parts.Region, parts.Service)

	if !hmac.Equal([]byte(expectedSig), []byte(parts.Signature)) {
		return "", fmt.Errorf("signature mismatch")
	}

	return parts.AccessKeyID, nil
}

// parseAuthHeader parses an AWS SigV4 Authorization header.
// Format: AWS4-HMAC-SHA256 Credential=AKID/date/region/service/aws4_request, SignedHeaders=h1;h2, Signature=hex
func parseAuthHeader(header string) (*AuthParts, error) {
	if !strings.HasPrefix(header, "AWS4-HMAC-SHA256 ") {
		return nil, fmt.Errorf("unsupported auth scheme")
	}

	body := header[len("AWS4-HMAC-SHA256 "):]
	fields := map[string]string{}

	for _, part := range strings.Split(body, ", ") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		fields[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}

	credential := fields["Credential"]
	credParts := strings.Split(credential, "/")
	if len(credParts) != 5 {
		return nil, fmt.Errorf("invalid credential: %q", credential)
	}

	signedHeaderStr := fields["SignedHeaders"]
	if signedHeaderStr == "" {
		return nil, fmt.Errorf("missing SignedHeaders")
	}

	signature := fields["Signature"]
	if signature == "" {
		return nil, fmt.Errorf("missing Signature")
	}

	return &AuthParts{
		AccessKeyID:   credParts[0],
		Date:          credParts[1],
		Region:        credParts[2],
		Service:       credParts[3],
		SignedHeaders: strings.Split(signedHeaderStr, ";"),
		Signature:     signature,
	}, nil
}

// computeSignature computes the AWS SigV4 signature for a request.
func computeSignature(r *http.Request, signedHeaders []string, secretKey, date, region, service string) string {
	cr := canonicalRequest(r, signedHeaders)
	crHash := hashSHA256([]byte(cr))

	amzDate := r.Header.Get("X-Amz-Date")
	stringToSign := "AWS4-HMAC-SHA256\n" +
		amzDate + "\n" +
		date + "/" + region + "/" + service + "/aws4_request\n" +
		crHash

	signingKey := deriveSigningKey(secretKey, date, region, service)
	return hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))
}

// canonicalRequest builds the canonical request string per AWS SigV4 spec.
func canonicalRequest(r *http.Request, signedHeaders []string) string {
	// Method
	method := r.Method

	// Canonical URI (path)
	canonicalURI := r.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalURI = uriEncode(canonicalURI, true)

	// Canonical query string
	canonicalQuery := canonicalQueryString(r.URL.Query())

	// Canonical headers
	sort.Strings(signedHeaders)
	var canonicalHeaders strings.Builder
	for _, h := range signedHeaders {
		val := strings.TrimSpace(r.Header.Get(h))
		if strings.EqualFold(h, "host") {
			val = r.Host
			if val == "" {
				val = r.Header.Get("Host")
			}
		}
		canonicalHeaders.WriteString(strings.ToLower(h))
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(val)
		canonicalHeaders.WriteByte('\n')
	}

	signedHeaderList := strings.Join(signedHeaders, ";")

	// Payload hash
	payloadHash := r.Header.Get("X-Amz-Content-Sha256")
	if payloadHash == "" {
		payloadHash = "UNSIGNED-PAYLOAD"
	}

	return method + "\n" +
		canonicalURI + "\n" +
		canonicalQuery + "\n" +
		canonicalHeaders.String() + "\n" +
		signedHeaderList + "\n" +
		payloadHash
}

// canonicalQueryString sorts query parameters and encodes them per AWS spec.
func canonicalQueryString(values url.Values) string {
	if len(values) == 0 {
		return ""
	}

	var pairs []string
	for k, vs := range values {
		for _, v := range vs {
			pairs = append(pairs, uriEncodeComponent(k)+"="+uriEncodeComponent(v))
		}
	}

	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

// deriveSigningKey derives the AWS SigV4 signing key.
func deriveSigningKey(secretKey, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func hashSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// uriEncode encodes a URI path per AWS S3 spec.
// If encodePath is true, forward slashes are preserved.
func uriEncode(path string, encodePath bool) string {
	var buf strings.Builder
	for _, c := range []byte(path) {
		if isUnreserved(c) || (encodePath && c == '/') {
			buf.WriteByte(c)
		} else {
			fmt.Fprintf(&buf, "%%%02X", c)
		}
	}
	return buf.String()
}

// uriEncodeComponent encodes a URI component (no slash preservation).
func uriEncodeComponent(s string) string {
	return uriEncode(s, false)
}

func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '.' || c == '_' || c == '~'
}
