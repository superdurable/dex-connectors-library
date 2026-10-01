// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package s3fake is a stateful, SigV4-verifying fake of the S3 REST subset the connector uses:
// ListObjectsV2, HeadObject, GetObject, and PutObject with If-None-Match. Connector and example tests
// share it so that both exercise the same signed wire format without a real store.
package s3fake

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// ErrorMessageSentinel appears in every fake error message, so tests can prove message text never escapes.
const ErrorMessageSentinel = "SENTINEL provider message text"

var authorizationPattern = regexp.MustCompile(
	`^AWS4-HMAC-SHA256 Credential=([^/,]+)/(\d{8})/([^/,]+)/s3/aws4_request, ?SignedHeaders=([a-z0-9;-]+), ?Signature=([0-9a-f]{64})$`)

// Config fixes the credentials, Region, and host the fake accepts.
type Config struct {
	// AccessKeyID, SecretAccessKey, and SessionToken are the only credentials the fake accepts.
	AccessKeyID, SecretAccessKey, SessionToken string
	// Region is the signing Region the fake accepts.
	Region string
	// Buckets lists the buckets that exist; requests for another bucket answer NoSuchBucket.
	Buckets []string
	// IsVersioned assigns a version ID to every stored object.
	IsVersioned bool
	// IgnoresIfNoneMatch makes PUT overwrite even with If-None-Match, as a store without conditional writes.
	IgnoresIfNoneMatch bool
}

// Object is one stored object.
type Object struct {
	// Body is the stored content.
	Body []byte
	// ContentType is the stored Content-Type.
	ContentType string
	// ContentEncoding is the stored Content-Encoding.
	ContentEncoding string
	// Metadata is the user-defined metadata keyed by lowercase name without the x-amz-meta- prefix.
	Metadata map[string]string
	// ETag is the quoted hex MD5 of Body.
	ETag string
	// VersionID is set when the fake is versioned.
	VersionID string
	// LastModified is when the object was stored.
	LastModified time.Time
}

// Request is one request the fake received after its body was read.
type Request struct {
	// Method is the HTTP method.
	Method string
	// Host is the Host header, which carries the bucket for virtual-hosted requests.
	Host string
	// EscapedPath is the path exactly as sent.
	EscapedPath string
	// Query is the decoded query.
	Query url.Values
	// Header is a copy of the request headers.
	Header http.Header
	// Body is the request body.
	Body []byte
}

// Response overrides the fake's answer to one request.
type Response struct {
	// StatusCode is the HTTP status.
	StatusCode int
	// Code is the S3 error code written in an XML error body; blank writes no body.
	Code string
	// Header holds extra response headers.
	Header http.Header
}

// Server is a running fake S3 endpoint. It is safe for concurrent use.
type Server struct {
	*httptest.Server
	config Config
	signer *v4.Signer

	mutex           sync.Mutex
	objects         map[string]Object
	requests        []Request
	interceptor     func(Request) *Response
	putDelays       []time.Duration
	nextVersion     int
	signatureErrors int
}

// NewServer starts a fake that closes when the test ends.
func NewServer(t testing.TB, config Config) *Server {
	t.Helper()
	server := &Server{
		config: config, objects: map[string]Object{},
		signer: v4.NewSigner(func(options *v4.SignerOptions) { options.DisableURIPathEscaping = true }),
	}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serveHTTP))
	t.Cleanup(server.Close)
	return server
}

// HostPort is the fake's listener address, such as 127.0.0.1:54321.
func (server *Server) HostPort() string { return server.Listener.Addr().String() }

// VirtualHostClient returns an HTTP client that sends every request, whatever its bucket host name, to the fake.
func (server *Server) VirtualHostClient() *http.Client {
	address := server.HostPort()
	transport := &http.Transport{DialContext: func(ctx context.Context, network string, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	return &http.Client{Transport: transport}
}

// SetObject stores an object directly, as another writer would.
func (server *Server) SetObject(bucket string, key string, object Object) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.objects[bucket+"/"+key] = server.completeObject(object)
}

// Object returns a stored object.
func (server *Server) Object(bucket string, key string) (Object, bool) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	object, isStored := server.objects[bucket+"/"+key]
	return object, isStored
}

// ObjectCount is the number of stored objects.
func (server *Server) ObjectCount() int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return len(server.objects)
}

// Requests returns every request received so far.
func (server *Server) Requests() []Request {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]Request(nil), server.requests...)
}

// RequestCount counts received requests with method; a blank method counts every request.
func (server *Server) RequestCount(method string) int {
	count := 0
	for _, request := range server.Requests() {
		if method == "" || request.Method == method {
			count++
		}
	}
	return count
}

// SignatureErrorCount is the number of requests whose signature did not verify.
func (server *Server) SignatureErrorCount() int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.signatureErrors
}

// Intercept answers matching requests with a fixed response before the fake applies them; a nil result
// lets the fake answer normally.
func (server *Server) Intercept(interceptor func(Request) *Response) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.interceptor = interceptor
}

// DelayPutResponses delays the response to the next PUT requests by the given durations after each write
// is applied, as a store whose success response arrives late.
func (server *Server) DelayPutResponses(delays ...time.Duration) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.putDelays = append(server.putDelays, delays...)
}

func (server *Server) serveHTTP(response http.ResponseWriter, httpRequest *http.Request) {
	body, err := io.ReadAll(httpRequest.Body)
	if err != nil {
		writeError(response, http.StatusBadRequest, "IncompleteBody")
		return
	}
	request := Request{
		Method: httpRequest.Method, Host: httpRequest.Host, EscapedPath: httpRequest.URL.EscapedPath(),
		Query: httpRequest.URL.Query(), Header: httpRequest.Header.Clone(), Body: body,
	}
	server.mutex.Lock()
	server.requests = append(server.requests, request)
	interceptor := server.interceptor
	server.mutex.Unlock()
	if code := server.verifySignature(httpRequest, body); code != "" {
		server.mutex.Lock()
		server.signatureErrors++
		server.mutex.Unlock()
		writeError(response, http.StatusForbidden, code)
		return
	}
	if interceptor != nil {
		if override := interceptor(request); override != nil {
			for name, values := range override.Header {
				response.Header()[name] = values
			}
			writeError(response, override.StatusCode, override.Code)
			return
		}
	}
	bucket, key, hasKey := server.splitLocation(httpRequest)
	if !server.hasBucket(bucket) {
		writeError(response, http.StatusNotFound, "NoSuchBucket")
		return
	}
	switch {
	case httpRequest.Method == http.MethodGet && !hasKey && request.Query.Get("list-type") == "2":
		server.listObjects(response, bucket, request.Query)
	case httpRequest.Method == http.MethodHead && hasKey:
		server.writeObject(response, bucket, key, false)
	case httpRequest.Method == http.MethodGet && hasKey:
		server.writeObject(response, bucket, key, true)
	case httpRequest.Method == http.MethodPut && hasKey:
		server.putObject(response, httpRequest, bucket, key, body)
	default:
		writeError(response, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// verifySignature recomputes the SigV4 signature from the request as received; a blank code means valid.
func (server *Server) verifySignature(request *http.Request, body []byte) string {
	match := authorizationPattern.FindStringSubmatch(request.Header.Get("Authorization"))
	if match == nil || match[1] != server.config.AccessKeyID {
		return "InvalidAccessKeyId"
	}
	if match[3] != server.config.Region {
		return "AuthorizationHeaderMalformed"
	}
	if request.Header.Get("X-Amz-Security-Token") != server.config.SessionToken {
		return "InvalidToken"
	}
	signingTime, err := time.Parse("20060102T150405Z", request.Header.Get("X-Amz-Date"))
	if err != nil || signingTime.Format("20060102") != match[2] {
		return "AuthorizationHeaderMalformed"
	}
	payloadHash := request.Header.Get("X-Amz-Content-Sha256")
	digest := sha256.Sum256(body)
	if payloadHash != hex.EncodeToString(digest[:]) {
		return "XAmzContentSHA256Mismatch"
	}
	signedHeaders := strings.Split(match[4], ";")
	for _, required := range []string{"host", "x-amz-content-sha256", "x-amz-date"} {
		if !contains(signedHeaders, required) {
			return "AccessDenied"
		}
	}
	for name := range request.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-amz-") && !contains(signedHeaders, strings.ToLower(name)) {
			return "AccessDenied"
		}
	}
	rebuilt := &http.Request{
		Method: request.Method, Host: request.Host, Header: http.Header{},
		URL: &url.URL{Scheme: "http", Host: request.Host, Path: request.URL.Path, RawPath: request.URL.EscapedPath(), RawQuery: request.URL.RawQuery},
	}
	for _, name := range signedHeaders {
		switch name {
		case "host":
		case "content-length":
			rebuilt.ContentLength = request.ContentLength
		default:
			rebuilt.Header[http.CanonicalHeaderKey(name)] = request.Header.Values(name)
		}
	}
	credentials := aws.Credentials{
		AccessKeyID: server.config.AccessKeyID, SecretAccessKey: server.config.SecretAccessKey, SessionToken: server.config.SessionToken,
	}
	if err := server.signer.SignHTTP(context.Background(), credentials, rebuilt, payloadHash, "s3", server.config.Region, signingTime); err != nil {
		return "SignatureDoesNotMatch"
	}
	if rebuilt.Header.Get("Authorization") != request.Header.Get("Authorization") {
		return "SignatureDoesNotMatch"
	}
	return ""
}

// splitLocation reads the bucket from a virtual host name or the first path segment.
func (server *Server) splitLocation(request *http.Request) (string, string, bool) {
	path := request.URL.Path
	if bucket, isVirtualHost := strings.CutSuffix(request.Host, "."+server.HostPort()); isVirtualHost {
		key := strings.TrimPrefix(path, "/")
		return bucket, key, key != ""
	}
	bucket, key, hasKey := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	return bucket, key, hasKey
}

func (server *Server) hasBucket(bucket string) bool {
	for _, existing := range server.config.Buckets {
		if existing == bucket {
			return true
		}
	}
	return false
}

func (server *Server) listObjects(response http.ResponseWriter, bucket string, query url.Values) {
	maxKeys, err := strconv.Atoi(query.Get("max-keys"))
	if err != nil || maxKeys < 1 || maxKeys > 1000 {
		writeError(response, http.StatusBadRequest, "InvalidArgument")
		return
	}
	prefix, delimiter, startAfter := query.Get("prefix"), query.Get("delimiter"), query.Get("start-after")
	if token := query.Get("continuation-token"); token != "" {
		decoded, err := base64.StdEncoding.DecodeString(token)
		if err != nil {
			writeError(response, http.StatusBadRequest, "InvalidArgument")
			return
		}
		startAfter = string(decoded)
	}
	server.mutex.Lock()
	var keys []string
	objects := map[string]Object{}
	for location, object := range server.objects {
		if key, isInBucket := strings.CutPrefix(location, bucket+"/"); isInBucket && strings.HasPrefix(key, prefix) && key > startAfter {
			keys = append(keys, key)
			objects[key] = object
		}
	}
	server.mutex.Unlock()
	sort.Strings(keys)
	isURLEncoded := query.Get("encoding-type") == "url"
	encode := func(value string) string {
		if isURLEncoded {
			return url.QueryEscape(value)
		}
		return value
	}
	var page bytes.Buffer
	page.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	if isURLEncoded {
		page.WriteString(`<EncodingType>url</EncodingType>`)
	}
	count, lastReturned, seenPrefixes := 0, "", map[string]bool{}
	isTruncated := false
	for _, key := range keys {
		entry := key
		if delimiter != "" {
			if index := strings.Index(key[len(prefix):], delimiter); index >= 0 {
				entry = key[:len(prefix)+index+len(delimiter)]
			}
		}
		if seenPrefixes[entry] {
			lastReturned = key
			continue
		}
		if count == maxKeys {
			isTruncated = true
			break
		}
		count++
		lastReturned = key
		if entry != key {
			seenPrefixes[entry] = true
			fmt.Fprintf(&page, `<CommonPrefixes><Prefix>%s</Prefix></CommonPrefixes>`, escapeXML(encode(entry)))
			continue
		}
		object := objects[key]
		fmt.Fprintf(&page, `<Contents><Key>%s</Key><LastModified>%s</LastModified><ETag>%s</ETag><Size>%d</Size><StorageClass>STANDARD</StorageClass></Contents>`,
			escapeXML(encode(key)), object.LastModified.UTC().Format("2006-01-02T15:04:05.000Z"), escapeXML(object.ETag), len(object.Body))
	}
	fmt.Fprintf(&page, `<KeyCount>%d</KeyCount><MaxKeys>%d</MaxKeys><IsTruncated>%t</IsTruncated>`, count, maxKeys, isTruncated)
	if isTruncated {
		fmt.Fprintf(&page, `<NextContinuationToken>%s</NextContinuationToken>`, base64.StdEncoding.EncodeToString([]byte(lastReturned)))
	}
	page.WriteString(`</ListBucketResult>`)
	response.Header().Set("Content-Type", "application/xml")
	response.Header().Set("X-Amz-Request-Id", "FAKEREQUEST0001")
	_, _ = response.Write(page.Bytes()) // A test client that stops reading only shortens its own response.
}

func (server *Server) writeObject(response http.ResponseWriter, bucket string, key string, isGet bool) {
	object, isStored := server.Object(bucket, key)
	if !isStored {
		if isGet {
			writeError(response, http.StatusNotFound, "NoSuchKey")
		} else {
			response.WriteHeader(http.StatusNotFound)
		}
		return
	}
	header := response.Header()
	header.Set("Content-Length", strconv.Itoa(len(object.Body)))
	header.Set("Last-Modified", object.LastModified.UTC().Format(http.TimeFormat))
	header.Set("ETag", object.ETag)
	header.Set("X-Amz-Request-Id", "FAKEREQUEST0002")
	if object.ContentType != "" {
		header.Set("Content-Type", object.ContentType)
	}
	if object.ContentEncoding != "" {
		header.Set("Content-Encoding", object.ContentEncoding)
	}
	if object.VersionID != "" {
		header.Set("X-Amz-Version-Id", object.VersionID)
	}
	for name, value := range object.Metadata {
		header.Set("X-Amz-Meta-"+name, value)
	}
	response.WriteHeader(http.StatusOK)
	if isGet {
		_, _ = response.Write(object.Body) // A test client that stops reading only shortens its own response.
	}
}

func (server *Server) putObject(response http.ResponseWriter, request *http.Request, bucket string, key string, body []byte) {
	digest := md5.Sum(body)
	if contentMD5 := request.Header.Get("Content-MD5"); contentMD5 != "" && contentMD5 != base64.StdEncoding.EncodeToString(digest[:]) {
		writeError(response, http.StatusBadRequest, "BadDigest")
		return
	}
	object := Object{
		Body: body, ContentType: request.Header.Get("Content-Type"), ContentEncoding: request.Header.Get("Content-Encoding"),
		Metadata: map[string]string{},
	}
	for name, values := range request.Header {
		if metadataName, isMetadata := strings.CutPrefix(name, "X-Amz-Meta-"); isMetadata {
			object.Metadata[strings.ToLower(metadataName)] = strings.Join(values, ",")
		}
	}
	server.mutex.Lock()
	location := bucket + "/" + key
	if _, exists := server.objects[location]; exists && request.Header.Get("If-None-Match") == "*" && !server.config.IgnoresIfNoneMatch {
		server.mutex.Unlock()
		writeError(response, http.StatusPreconditionFailed, "PreconditionFailed")
		return
	}
	stored := server.completeObject(object)
	server.objects[location] = stored
	var delay time.Duration
	if len(server.putDelays) > 0 {
		delay, server.putDelays = server.putDelays[0], server.putDelays[1:]
	}
	server.mutex.Unlock()
	if delay > 0 {
		time.Sleep(delay) // The fake applies the write first, then answers late, as a slow store would.
	}
	response.Header().Set("ETag", stored.ETag)
	response.Header().Set("X-Amz-Request-Id", "FAKEREQUEST0003")
	if stored.VersionID != "" {
		response.Header().Set("X-Amz-Version-Id", stored.VersionID)
	}
	response.WriteHeader(http.StatusOK)
}

// completeObject fills the ETag, version, and time; the caller holds the mutex.
func (server *Server) completeObject(object Object) Object {
	digest := md5.Sum(object.Body)
	object.ETag = `"` + hex.EncodeToString(digest[:]) + `"`
	if object.LastModified.IsZero() {
		object.LastModified = time.Now().UTC().Truncate(time.Second)
	}
	if server.config.IsVersioned {
		server.nextVersion++
		object.VersionID = fmt.Sprintf("fake-version-%04d", server.nextVersion)
	}
	return object
}

// writeError writes an S3 XML error whose message text is the sentinel; a blank code writes no body.
func writeError(response http.ResponseWriter, statusCode int, code string) {
	if code == "" {
		response.WriteHeader(statusCode)
		return
	}
	var document bytes.Buffer
	document.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	_ = xml.NewEncoder(&document).Encode(struct {
		XMLName   xml.Name `xml:"Error"`
		Code      string   `xml:"Code"`
		Message   string   `xml:"Message"`
		RequestID string   `xml:"RequestId"`
	}{Code: code, Message: ErrorMessageSentinel, RequestID: "FAKEREQUEST0004"}) // Encoding fixed strings cannot fail.
	response.Header().Set("Content-Type", "application/xml")
	response.Header().Set("X-Amz-Request-Id", "FAKEREQUEST0004")
	response.WriteHeader(statusCode)
	_, _ = response.Write(document.Bytes()) // A test client that stops reading only shortens its own response.
}

func escapeXML(value string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(value)) // Writing to a bytes.Buffer cannot fail.
	return escaped.String()
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
