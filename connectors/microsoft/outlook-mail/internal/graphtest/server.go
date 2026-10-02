// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package graphtest is a stateful stand-in for the Microsoft Graph v1.0 mail API and the Microsoft identity
// platform token endpoints, for the connector's tests and example. It keeps one mailbox, issues delegated
// and app-only tokens, rotates refresh tokens, and can delay, refuse, or drop any request.
//
// Like Exchange Online it keeps immutable message IDs across moves and sends, stores single-value extended
// properties, and finds messages by them. It requires the Prefer: IdType="ImmutableId" header on every
// Graph request, so a test fails if the connector stops asking for immutable IDs. Every error message
// contains SentinelText, so tests can prove the connector never repeats Graph's message text.
package graphtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// SentinelText appears in every error message the fake returns.
const SentinelText = "GRAPH-SENTINEL-SERVER-TEXT"

const maximumRequestBodyBytes = 8 << 20

// Endpoint names one kind of request the fake counts and can fail.
type Endpoint string

const (
	// EndpointDelegatedToken is a refresh-token grant at the organizations token endpoint.
	EndpointDelegatedToken Endpoint = "delegatedToken"
	// EndpointAppOnlyToken is a client credentials grant at a tenant's token endpoint.
	EndpointAppOnlyToken Endpoint = "appOnlyToken"
	// EndpointListFolderMessages is GET /mailFolders/{folder}/messages.
	EndpointListFolderMessages Endpoint = "listFolderMessages"
	// EndpointFindMessagesByMarker is GET /messages filtered by an extended property.
	EndpointFindMessagesByMarker Endpoint = "findMessagesByMarker"
	// EndpointGetMessage is GET /messages/{id}.
	EndpointGetMessage Endpoint = "getMessage"
	// EndpointListAttachments is GET /messages/{id}/attachments.
	EndpointListAttachments Endpoint = "listAttachments"
	// EndpointCreateDraft is POST /messages.
	EndpointCreateDraft Endpoint = "createDraft"
	// EndpointCreateReply is POST /messages/{id}/createReply or createReplyAll.
	EndpointCreateReply Endpoint = "createReply"
	// EndpointUpdateMessage is PATCH /messages/{id}.
	EndpointUpdateMessage Endpoint = "updateMessage"
	// EndpointSendDraft is POST /messages/{id}/send.
	EndpointSendDraft Endpoint = "sendDraft"
	// EndpointMoveMessage is POST /messages/{id}/move.
	EndpointMoveMessage Endpoint = "moveMessage"
	// EndpointGetMailFolder is GET /mailFolders/{folder}.
	EndpointGetMailFolder Endpoint = "getMailFolder"
)

// ServerConfig holds the credentials the fake accepts.
type ServerConfig struct {
	// MailboxAddress is the mailbox's primary address; /me and /users/<address> both reach it.
	MailboxAddress string
	// ClientID and ClientSecret are the app registration both grants must present in the form body.
	ClientID     string
	ClientSecret string
	// RefreshToken is the delegated refresh token valid at start; every refresh rotates it.
	RefreshToken string
	// TenantID is the only tenant whose client credentials grant succeeds.
	TenantID string
	// GrantedScope is the scope a refresh returns; empty returns "Mail.ReadWrite Mail.Send".
	GrantedScope string
}

// Fault changes how the fake answers matching requests.
type Fault struct {
	// Endpoint selects the requests the fault affects.
	Endpoint Endpoint
	// Count is how many matching requests the fault affects; zero means one.
	Count int
	// Delay waits before the request is handled, while it is still in flight.
	Delay time.Duration
	// HoldUntil, when set, waits until the channel closes before the request is handled.
	HoldUntil <-chan struct{}
	// Status answers with this HTTP status and a Graph error body instead of handling the request.
	Status int
	// ErrorCode is the error.code of that body.
	ErrorCode string
	// RetryAfter is the Retry-After header of that answer.
	RetryAfter string
	// ShouldApplyFirst handles the request before answering with Status or dropping the connection.
	ShouldApplyFirst bool
	// ShouldDropConnection closes the connection instead of answering.
	ShouldDropConnection bool
}

// RecordedRequest is one request the fake received, without its Authorization header.
type RecordedRequest struct {
	Method  string
	Path    string
	Query   url.Values
	Header  http.Header
	Body    []byte
	Arrived time.Time
}

// Server is a running fake. All methods are safe for concurrent use.
type Server struct {
	URL string

	config     ServerConfig
	httpServer *httptest.Server
	mutex      sync.Mutex
	mailbox    *mailboxState
	tokens     map[string]tokenGrant
	refresh    string
	sequence   int
	faults     []*Fault
	requests   map[Endpoint][]RecordedRequest
	inFlight   sync.WaitGroup
	now        func() time.Time
	// sendCompletionDelay keeps an accepted draft a draft for this long, like Exchange's asynchronous send.
	sendCompletionDelay time.Duration
	// ignoresTextBodyPreference returns HTML bodies even when the request prefers text.
	ignoresTextBodyPreference bool
}

type tokenGrant struct {
	isAppOnly bool
	isExpired bool
}

// Start runs a fake on a loopback port until the test ends.
func Start(t testing.TB, config ServerConfig) *Server {
	t.Helper()
	if config.GrantedScope == "" {
		config.GrantedScope = "Mail.ReadWrite Mail.Send"
	}
	server := &Server{
		config: config, mailbox: newMailboxState(config.MailboxAddress), tokens: map[string]tokenGrant{},
		refresh: config.RefreshToken, requests: map[Endpoint][]RecordedRequest{}, now: time.Now,
	}
	server.httpServer = httptest.NewServer(http.HandlerFunc(server.serveHTTP))
	server.URL = server.httpServer.URL
	t.Cleanup(func() {
		server.inFlight.Wait()
		server.httpServer.Close()
	})
	return server
}

// InjectFault adds a fault; faults apply in the order they were added.
func (server *Server) InjectFault(fault Fault) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if fault.Count == 0 {
		fault.Count = 1
	}
	server.faults = append(server.faults, &fault)
}

// SetSendCompletionDelay keeps every accepted draft a draft for delay before it appears in Sent Items.
func (server *Server) SetSendCompletionDelay(delay time.Duration) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.sendCompletionDelay = delay
}

// CompletePendingSends moves every accepted draft to Sent Items now.
func (server *Server) CompletePendingSends() {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	for _, message := range server.mailbox.messages {
		if message.sendCompletesAt != nil {
			server.mailbox.completeSend(message, server.now())
		}
	}
}

// IgnoreTextBodyPreference makes the fake return HTML bodies even when text is preferred.
func (server *Server) IgnoreTextBodyPreference() {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.ignoresTextBodyPreference = true
}

// ExpireAccessTokens makes every issued access token fail with 401 until a new one is issued.
func (server *Server) ExpireAccessTokens() {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	for token, grant := range server.tokens {
		grant.isExpired = true
		server.tokens[token] = grant
	}
}

// CurrentRefreshToken returns the refresh token the next delegated refresh must present.
func (server *Server) CurrentRefreshToken() string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.refresh
}

// RequestCount returns how many requests reached endpoint, including refused ones.
func (server *Server) RequestCount(endpoint Endpoint) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return len(server.requests[endpoint])
}

// Requests returns the requests that reached endpoint, in arrival order.
func (server *Server) Requests(endpoint Endpoint) []RecordedRequest {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]RecordedRequest(nil), server.requests[endpoint]...)
}

// WaitForHeldRequests waits until every delayed or held request has finished.
func (server *Server) WaitForHeldRequests() { server.inFlight.Wait() }

func (server *Server) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	server.inFlight.Add(1)
	defer server.inFlight.Done()
	body, err := readRequestBody(request)
	if err != nil {
		writeGraphError(writer, http.StatusBadRequest, "RequestBodyRead")
		return
	}
	segments, err := escapedPathSegments(request)
	if err != nil {
		writeGraphError(writer, http.StatusBadRequest, "BadRequest")
		return
	}
	if len(segments) == 4 && segments[1] == "oauth2" && segments[2] == "v2.0" && segments[3] == "token" && request.Method == http.MethodPost {
		server.serveToken(writer, request, segments[0], body)
		return
	}
	server.serveGraph(writer, request, segments, body)
}

// serveToken handles both grants with the client secret in the form body, as Microsoft documents.
func (server *Server) serveToken(writer http.ResponseWriter, request *http.Request, tenant string, body []byte) {
	form, err := url.ParseQuery(string(body))
	if err != nil {
		writeTokenError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	endpoint := EndpointAppOnlyToken
	if tenant == "organizations" {
		endpoint = EndpointDelegatedToken
	}
	fault := server.recordRequest(endpoint, request, body)
	if server.applyFaultBeforeHandling(writer, fault) {
		return
	}
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if form.Get("client_id") != server.config.ClientID || form.Get("client_secret") != server.config.ClientSecret {
		writeTokenError(writer, http.StatusUnauthorized, "invalid_client")
		return
	}
	server.sequence++
	switch {
	case endpoint == EndpointDelegatedToken && form.Get("grant_type") == "refresh_token":
		if form.Get("refresh_token") != server.refresh {
			writeTokenError(writer, http.StatusBadRequest, "invalid_grant")
			return
		}
		accessToken := fmt.Sprintf("delegated-access-%d", server.sequence)
		server.tokens[accessToken] = tokenGrant{}
		server.refresh = fmt.Sprintf("rotated-refresh-%d", server.sequence)
		writeJSON(writer, http.StatusOK, map[string]any{
			"token_type": "Bearer", "scope": server.config.GrantedScope, "expires_in": 3599, "ext_expires_in": 3599,
			"access_token": accessToken, "refresh_token": server.refresh,
		})
	case endpoint == EndpointAppOnlyToken && form.Get("grant_type") == "client_credentials":
		if tenant != server.config.TenantID {
			writeTokenError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
		if form.Get("scope") != "https://graph.microsoft.com/.default" {
			writeTokenError(writer, http.StatusBadRequest, "invalid_scope")
			return
		}
		accessToken := fmt.Sprintf("app-only-access-%d", server.sequence)
		server.tokens[accessToken] = tokenGrant{isAppOnly: true}
		writeJSON(writer, http.StatusOK, map[string]any{"token_type": "Bearer", "expires_in": 3599, "ext_expires_in": 3599, "access_token": accessToken})
	default:
		writeTokenError(writer, http.StatusBadRequest, "unsupported_grant_type")
	}
}

// IssueDelegatedAccessToken returns a valid delegated token, as Dex Web stores one after consent.
func (server *Server) IssueDelegatedAccessToken() string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.sequence++
	token := fmt.Sprintf("delegated-access-%d", server.sequence)
	server.tokens[token] = tokenGrant{}
	return token
}

// recordRequest stores the request and returns the first matching fault, consuming one use of it.
func (server *Server) recordRequest(endpoint Endpoint, request *http.Request, body []byte) *Fault {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	header := request.Header.Clone()
	header.Del("Authorization")
	server.requests[endpoint] = append(server.requests[endpoint], RecordedRequest{
		Method: request.Method, Path: request.URL.Path, Query: request.URL.Query(), Header: header, Body: body, Arrived: server.now(),
	})
	for index, fault := range server.faults {
		if fault.Endpoint != endpoint {
			continue
		}
		matched := *fault
		fault.Count--
		if fault.Count == 0 {
			server.faults = append(server.faults[:index], server.faults[index+1:]...)
		}
		return &matched
	}
	return nil
}

// applyFaultBeforeHandling waits for a delay or hold and answers a fault that handles nothing.
func (server *Server) applyFaultBeforeHandling(writer http.ResponseWriter, fault *Fault) bool {
	if fault == nil {
		return false
	}
	if fault.Delay > 0 {
		time.Sleep(fault.Delay)
	}
	if fault.HoldUntil != nil {
		<-fault.HoldUntil
	}
	if fault.ShouldApplyFirst {
		return false
	}
	return answerFault(writer, fault)
}

// answerFault writes the fault's answer; it reports false when the fault only delays.
func answerFault(writer http.ResponseWriter, fault *Fault) bool {
	switch {
	case fault.ShouldDropConnection:
		dropConnection(writer)
		return true
	case fault.Status != 0:
		if fault.RetryAfter != "" {
			writer.Header().Set("Retry-After", fault.RetryAfter)
		}
		writeGraphError(writer, fault.Status, fault.ErrorCode)
		return true
	default:
		return false
	}
}

// faultAnsweringWriter defers a ShouldApplyFirst fault's answer until the request has been handled.
type faultAnsweringWriter struct {
	http.ResponseWriter
	fault *Fault
}

func (writer faultAnsweringWriter) answerInsteadOf(write func(http.ResponseWriter)) {
	if writer.fault != nil && writer.fault.ShouldApplyFirst && answerFault(writer.ResponseWriter, writer.fault) {
		return
	}
	write(writer.ResponseWriter)
}

func dropConnection(writer http.ResponseWriter) {
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		panic("graphtest: the response writer cannot drop its connection")
	}
	connection, _, err := hijacker.Hijack()
	if err != nil {
		panic(err)
	}
	// Closing is the fault; its error cannot change the dropped answer.
	_ = connection.(*net.TCPConn).SetLinger(0)
	_ = connection.Close()
}

func readRequestBody(request *http.Request) ([]byte, error) {
	if request.Body == nil {
		return nil, nil
	}
	defer request.Body.Close()
	body, err := io.ReadAll(io.LimitReader(request.Body, maximumRequestBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maximumRequestBodyBytes {
		return nil, fmt.Errorf("request body is too large")
	}
	return body, nil
}

// escapedPathSegments splits the escaped path, so an ID containing an escaped slash stays one segment.
func escapedPathSegments(request *http.Request) ([]string, error) {
	var segments []string
	for _, escaped := range strings.Split(strings.Trim(request.URL.EscapedPath(), "/"), "/") {
		segment, err := url.PathUnescape(escaped)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("request-id", "0d5e3b4f-1a2b-4c3d-8e9f-001122334455")
	writer.WriteHeader(status)
	// A failed write means the client went away; the test sees the transport error itself.
	_ = json.NewEncoder(writer).Encode(value)
}

func writeGraphError(writer http.ResponseWriter, status int, code string) {
	if code == "" {
		code = "UnknownError"
	}
	writeJSON(writer, status, map[string]any{"error": map[string]any{
		"code": code, "message": "Graph says " + SentinelText + " for status " + strconv.Itoa(status),
		"innerError": map[string]any{"request-id": "0d5e3b4f-1a2b-4c3d-8e9f-001122334455", "date": "2026-10-01T09:00:00"},
	}})
}

func writeTokenError(writer http.ResponseWriter, status int, code string) {
	writeJSON(writer, status, map[string]any{
		"error": code, "error_description": "AADSTS700000: " + SentinelText, "error_codes": []int{700000},
		"trace_id": "8f0a1d4e-0000-0000-0000-000000000000", "correlation_id": "8f0a1d4e-0000-0000-0000-000000000001",
	})
}
