// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// sentinelSecret stands in for every client secret; no Result, receipt, or failure may contain it.
	sentinelSecret = "SENTINEL-CLIENT-SECRET"
	testClientID   = "dex-test-client"
)

var testConnection = sdkgo.ConnectionRef{Provider: "oauth2-http", Name: "protected-api"}

// receivedRequest is one request that reached the fake API.
type receivedRequest struct {
	method        string
	path          string
	query         url.Values
	header        http.Header
	body          string
	authorization string
}

// fakeAuthorizationServer is a plain-http server that is both the OAuth token endpoint and the protected
// API, as a Keycloak realm is. A refresh token works once: the server rotates it on use, so reusing the
// old one is invalid_grant.
type fakeAuthorizationServer struct {
	*httptest.Server
	mu                  sync.Mutex
	validAccessTokens   map[string]bool
	validRefreshTokens  map[string]bool
	issuedTokenCount    int
	accessTokenLifetime int
	tokenRequests       []url.Values
	tokenBasicClients   []string
	tokenStatus         int
	apiRequests         []receivedRequest
	// api answers requests whose bearer token is valid; nil answers 200 with a JSON document.
	api http.HandlerFunc
	// onUnauthorized runs when the server rejects a bearer token, before it answers 401.
	onUnauthorized func()
	// rejectsEveryToken makes the API answer 401 whatever the bearer token is.
	rejectsEveryToken bool
}

func newFakeAuthorizationServer(t *testing.T) *fakeAuthorizationServer {
	t.Helper()
	server := &fakeAuthorizationServer{
		validAccessTokens: map[string]bool{}, validRefreshTokens: map[string]bool{}, accessTokenLifetime: 60,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", server.serveToken)
	mux.HandleFunc("/", server.serveAPI)
	server.Server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// issueTokens starts a session and returns the access and refresh tokens of its first grant.
func (server *fakeAuthorizationServer) issueTokens() (string, string) {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.issueTokensLocked()
}

func (server *fakeAuthorizationServer) issueTokensLocked() (string, string) {
	server.issuedTokenCount++
	access, refresh := fmt.Sprintf("access-token-%d", server.issuedTokenCount), fmt.Sprintf("refresh-token-%d", server.issuedTokenCount)
	server.validAccessTokens[access], server.validRefreshTokens[refresh] = true, true
	return access, refresh
}

func (server *fakeAuthorizationServer) expireAccessToken(token string) {
	server.mu.Lock()
	defer server.mu.Unlock()
	delete(server.validAccessTokens, token)
}

func (server *fakeAuthorizationServer) serveToken(response http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		http.Error(response, "bad form", http.StatusBadRequest)
		return
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	server.tokenRequests = append(server.tokenRequests, request.PostForm)
	if username, password, hasBasicAuth := request.BasicAuth(); hasBasicAuth {
		server.tokenBasicClients = append(server.tokenBasicClients, username+":"+password)
	}
	response.Header().Set("Content-Type", "application/json")
	if server.tokenStatus != 0 {
		response.WriteHeader(server.tokenStatus)
		_, _ = io.WriteString(response, `{"error":"temporarily_unavailable"}`)
		return
	}
	refreshToken := request.PostForm.Get("refresh_token")
	if request.PostForm.Get("grant_type") != "refresh_token" || !server.validRefreshTokens[refreshToken] {
		response.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(response, `{"error":"invalid_grant","error_description":"Token is not active"}`)
		return
	}
	delete(server.validRefreshTokens, refreshToken)
	access, refresh := server.issueTokensLocked()
	_ = json.NewEncoder(response).Encode(map[string]any{
		"access_token": access, "refresh_token": refresh, "expires_in": server.accessTokenLifetime, "token_type": "Bearer",
	})
}

func (server *fakeAuthorizationServer) serveAPI(response http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	received := receivedRequest{
		method: request.Method, path: request.URL.EscapedPath(), query: request.URL.Query(), header: request.Header.Clone(),
		body: string(body), authorization: request.Header.Get("Authorization"),
	}
	server.mu.Lock()
	server.apiRequests = append(server.apiRequests, received)
	isAuthorized := server.validAccessTokens[strings.TrimPrefix(received.authorization, "Bearer ")] && !server.rejectsEveryToken
	handler, onUnauthorized := server.api, server.onUnauthorized
	server.mu.Unlock()
	if !isAuthorized {
		if onUnauthorized != nil {
			onUnauthorized()
		}
		response.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		response.WriteHeader(http.StatusUnauthorized)
		return
	}
	if handler != nil {
		handler(response, request)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(response, `{"sub":"user-1"}`)
}

func (server *fakeAuthorizationServer) requests() []receivedRequest {
	server.mu.Lock()
	defer server.mu.Unlock()
	return append([]receivedRequest(nil), server.apiRequests...)
}

func (server *fakeAuthorizationServer) refreshRequests() []url.Values {
	server.mu.Lock()
	defer server.mu.Unlock()
	return append([]url.Values(nil), server.tokenRequests...)
}

func (server *fakeAuthorizationServer) config() oauth2http.Config {
	return oauth2http.Config{
		AuthorizationEndpoint: server.URL + "/authorize", TokenEndpoint: server.URL + "/token",
		Scopes: []string{"openid"}, APIBaseURL: server.URL + "/api",
	}
}

// testCredentials returns the credential value that holds the given tokens.
func testCredentials(accessToken string, refreshToken string) oauth2http.Credentials {
	return oauth2http.Credentials{
		OAuthClientID: testClientID, OAuthClientSecret: sdkgo.NewSecretString(sentinelSecret),
		AccessToken: sdkgo.NewSecretString(accessToken), RefreshToken: sdkgo.NewSecretString(refreshToken),
	}
}

// testSession is a client wired to a fake server through an in-memory refreshing credential provider.
type testSession struct {
	server      *fakeAuthorizationServer
	client      *oauth2http.Client
	credentials *testsupport.RefreshingCredentials[oauth2http.Credentials]
	clock       *testClock
}

// testClock is a clock that tests advance by hand.
type testClock struct {
	mu      sync.Mutex
	current time.Time
}

func (clock *testClock) now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.current
}

func (clock *testClock) advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.current = clock.current.Add(duration)
}

var testStart = time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)

// newTestSession issues one grant and stores it with an expiry lifetime after the clock's start.
func newTestSession(t *testing.T, mutateConfig func(*oauth2http.Config), lifetime time.Duration) *testSession {
	t.Helper()
	server := newFakeAuthorizationServer(t)
	access, refresh := server.issueTokens()
	clock := &testClock{current: testStart}
	expiresAt := testStart.Add(lifetime)
	credentials := testsupport.NewRefreshingCredentials(testCredentials(access, refresh), &expiresAt, clock.now)
	config := server.config()
	if mutateConfig != nil {
		mutateConfig(&config)
	}
	client, err := oauth2http.New(config, credentials, oauth2http.WithClock(clock.now), oauth2http.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	return &testSession{server: server, client: client, credentials: credentials, clock: clock}
}

// send runs one sendRequest Step execution; the step name identifies the Step execution.
func (session *testSession) send(t *testing.T, step string, input oauth2http.SendRequestInput) (oauth2http.SendRequestResult, error) {
	t.Helper()
	return sendRequest(t, session.client, step, input)
}

func sendRequest(t *testing.T, client *oauth2http.Client, step string, input oauth2http.SendRequestInput) (oauth2http.SendRequestResult, error) {
	t.Helper()
	return sdkgo.RunMutation(testsupport.NewDexContext("oauth2-http-flow", step), client.SendRequest(), testConnection, input)
}

// requireSecretFree fails when a Result holds a client secret or an access or refresh token.
func requireSecretFree(t *testing.T, result oauth2http.SendRequestResult, tokens ...string) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	for _, secret := range append([]string{sentinelSecret}, tokens...) {
		require.NotContains(t, string(encoded), secret)
	}
}
