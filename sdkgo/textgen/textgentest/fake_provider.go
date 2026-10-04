// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgentest

import (
	"bytes"
	"cmp"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

const (
	// maxRecordedRequestBytes bounds how much of one request body the fake keeps.
	maxRecordedRequestBytes = 16 << 20
	redactedCredential      = "[REDACTED]"
)

// FakeReply is one scripted provider reply. The zero value is an empty 200
// response.
type FakeReply struct {
	// StatusCode is the HTTP status. Zero sends 200.
	StatusCode int
	// Header holds response headers, such as Content-Type or Retry-After.
	Header http.Header
	// Body is written and flushed right after the status line.
	Body string
	// Delay is how long the provider stays silent before it sends the status
	// line, or before it drops the connection.
	Delay time.Duration
	// StreamChunks are written and flushed in order after Body, each after its
	// own delay, so a reply can model keep-alives and slow streams.
	StreamChunks []FakeStreamChunk
	// ShouldDropConnection closes the connection without any response, which
	// the client sees as a transport error.
	ShouldDropConnection bool
}

// FakeStreamChunk is one piece of a streamed reply body.
type FakeStreamChunk struct {
	// Delay is how long the provider waits before writing Data.
	Delay time.Duration
	// Data is written and flushed as one piece.
	Data string
}

// RecordedRequest is one request the fake provider received. The credential
// header is removed, and any other occurrence of the credential is replaced
// with "[REDACTED]", so printing a record never reveals it.
type RecordedRequest struct {
	// Method is the HTTP method, such as POST.
	Method string
	// Path is the request path and query as sent, such as "/v1/chat/completions".
	Path string
	// Header holds the request headers except the credential header.
	Header http.Header
	// Body is the request body, truncated at 16 MiB.
	Body []byte
	// ReceivedAt is when the provider received the request.
	ReceivedAt time.Time
	// HasCredentialInSlot reports whether the credential header held exactly
	// the expected prefix and credential.
	HasCredentialInSlot bool
	// HasCredentialOutsideSlot reports whether the credential appeared
	// anywhere else: the path, another header, or the body.
	HasCredentialOutsideSlot bool
}

// FakeProvider is a scripted HTTP provider on a loopback address. It answers
// requests with queued replies in order and records each request without the
// credential, so test failures and logs never print it. When the queue is
// empty it answers 500 with the error type "llmtest_no_scripted_reply".
//
// A FakeProvider is safe for concurrent use.
type FakeProvider struct {
	server           *httptest.Server
	credentialHeader textgen.CredentialHeader
	credential       string
	mu               sync.Mutex
	replies          []FakeReply
	requests         []RecordedRequest
}

// NewFakeProvider starts a fake provider that expects credential in
// credentialHeader, such as Authorization with the "Bearer " prefix. The
// provider closes when the test ends. A delaying reply stops as soon as its
// client disconnects.
func NewFakeProvider(t testing.TB, credentialHeader textgen.CredentialHeader, credential string) *FakeProvider {
	t.Helper()
	if credentialHeader.Name == "" || credential == "" {
		t.Fatal("llmtest fake provider requires a credential header name and a credential")
	}
	provider := &FakeProvider{credentialHeader: credentialHeader, credential: credential}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.server.Close)
	return provider
}

// BaseURL returns the provider's loopback base URL without a trailing slash.
func (provider *FakeProvider) BaseURL() string { return provider.server.URL }

// EnqueueReplies appends replies to the queue that later requests consume in order.
func (provider *FakeProvider) EnqueueReplies(replies ...FakeReply) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.replies = append(provider.replies, replies...)
}

// Requests returns every request received so far, in arrival order.
func (provider *FakeProvider) Requests() []RecordedRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]RecordedRequest(nil), provider.requests...)
}

func (provider *FakeProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(io.LimitReader(request.Body, maxRecordedRequestBytes))
	if err != nil {
		// The client abandoned the request; there is nothing to answer.
		return
	}
	reply, hasReply := provider.recordRequestAndTakeReply(request, body)
	if !hasReply {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(response, `{"error":{"type":"llmtest_no_scripted_reply"}}`)
		return
	}
	if !waitOrStop(request, reply.Delay) {
		return
	}
	if reply.ShouldDropConnection {
		dropConnection(response)
		return
	}
	for name, values := range reply.Header {
		for _, value := range values {
			response.Header().Add(name, value)
		}
	}
	response.WriteHeader(cmp.Or(reply.StatusCode, http.StatusOK))
	// Write errors mean the client went away, which ends the reply either way.
	_, _ = io.WriteString(response, reply.Body)
	flushResponse(response)
	for _, chunk := range reply.StreamChunks {
		if !waitOrStop(request, chunk.Delay) {
			return
		}
		if _, err := io.WriteString(response, chunk.Data); err != nil {
			return
		}
		flushResponse(response)
	}
}

func (provider *FakeProvider) recordRequestAndTakeReply(request *http.Request, body []byte) (FakeReply, bool) {
	expectedValue := provider.credentialHeader.Prefix + provider.credential
	header := request.Header.Clone()
	slotValues := header.Values(provider.credentialHeader.Name)
	header.Del(provider.credentialHeader.Name)
	path := request.URL.RequestURI()
	recorded := RecordedRequest{
		Method: request.Method, ReceivedAt: time.Now(),
		HasCredentialInSlot: len(slotValues) == 1 && slotValues[0] == expectedValue,
		HasCredentialOutsideSlot: strings.Contains(path, provider.credential) ||
			bytes.Contains(body, []byte(provider.credential)) || headerContains(header, provider.credential),
		Path:   strings.ReplaceAll(path, provider.credential, redactedCredential),
		Header: redactHeader(header, provider.credential),
		Body:   bytes.ReplaceAll(body, []byte(provider.credential), []byte(redactedCredential)),
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.requests = append(provider.requests, recorded)
	if len(provider.replies) == 0 {
		return FakeReply{}, false
	}
	reply := provider.replies[0]
	provider.replies = provider.replies[1:]
	return reply, true
}

func redactHeader(header http.Header, credential string) http.Header {
	redacted := make(http.Header, len(header))
	for name, values := range header {
		name = strings.ReplaceAll(name, credential, redactedCredential)
		for _, value := range values {
			redacted[name] = append(redacted[name], strings.ReplaceAll(value, credential, redactedCredential))
		}
	}
	return redacted
}

func headerContains(header http.Header, value string) bool {
	for name, values := range header {
		if strings.Contains(name, value) {
			return true
		}
		for _, headerValue := range values {
			if strings.Contains(headerValue, value) {
				return true
			}
		}
	}
	return false
}

// waitOrStop waits for delay and reports false when the client or server ends the request first.
func waitOrStop(request *http.Request, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-request.Context().Done():
		return false
	}
}

func dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	if !isHijacker {
		panic("llmtest fake provider requires a hijackable connection")
	}
	connection, _, err := hijacker.Hijack()
	if err != nil {
		return
	}
	// Closing is the point; a close error leaves the client with the same transport failure.
	_ = connection.Close()
}

func flushResponse(response http.ResponseWriter) {
	if flusher, isFlusher := response.(http.Flusher); isFlusher {
		flusher.Flush()
	}
}
