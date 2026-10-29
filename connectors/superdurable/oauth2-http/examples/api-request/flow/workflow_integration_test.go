//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apirequest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	initialAccessToken  = "integration-access-token-0"
	initialRefreshToken = "integration-refresh-token-0"
)

// TestAPIRequestExampleRefreshesTheTokenAndReadsTheAPIWithRealDex runs the Flow on a real Worker against a
// plain-http authorization server whose stored access token has expired.
func TestAPIRequestExampleRefreshesTheTokenAndReadsTheAPIWithRealDex(t *testing.T) {
	server := newAuthorizationServer(t)
	flow, harness := newAPIRequestHarness(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "oauth2-http-api-request-success-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	requestID := "start-" + flowID
	options := dex.StartFlowOptions{RequestID: &requestID, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}}

	runID, err := harness.client.StartFlow(ctx, flow, flowID, Input{Path: "/userinfo"}, options)
	require.NoError(t, err)
	repeatedRunID, err := harness.client.StartFlow(ctx, flow, flowID, Input{Path: "/userinfo"}, options)
	require.NoError(t, err, "a retry of the same logical start attaches to the existing run")
	require.Equal(t, runID, repeatedRunID)

	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var report Report
	require.NoError(t, result.DecodeSingleOutput(&report))
	require.Equal(t, StatusCompleted, report.Status)
	require.Equal(t, http.StatusOK, report.StatusCode)
	require.JSONEq(t, `{"sub":"user-1","preferred_username":"alice"}`, report.Body)
	require.Equal(t, 1, server.refreshCount(), "the stored token had expired, so it was refreshed once before the call")
	require.Equal(t, []string{"Bearer integration-access-token-1"}, server.bearerTokens("/userinfo"))
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	for _, secret := range []string{initialAccessToken, initialRefreshToken, "integration-access-token-1", "integration-client-secret"} {
		require.NotContains(t, string(encoded), secret)
	}
}

// TestAPIRequestExampleFailsAnUnwiredRejectionWithRealDex verifies the API refusal is terminal without retry.
func TestAPIRequestExampleFailsAnUnwiredRejectionWithRealDex(t *testing.T) {
	server := newAuthorizationServer(t)
	flow, harness := newAPIRequestHarness(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "oauth2-http-api-request-rejected-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	_, err := harness.client.StartFlow(ctx, flow, flowID, Input{Path: "/missing"}, dex.StartFlowOptions{})
	require.NoError(t, err)

	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "rejected")
	require.Contains(t, result.ErrorMessage, "no target")
	require.Len(t, server.bearerTokens("/missing"), 1, "a rejection is not retried")
}

// TestAPIRequestExampleRetriesAThrottledRequestAfterTheServersDelayWithRealDex lets Dex honor Retry-After.
func TestAPIRequestExampleRetriesAThrottledRequestAfterTheServersDelayWithRealDex(t *testing.T) {
	server := newAuthorizationServer(t)
	flow, harness := newAPIRequestHarness(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "oauth2-http-api-request-throttled-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	_, err := harness.client.StartFlow(ctx, flow, flowID, Input{Path: "/throttled"}, dex.StartFlowOptions{})
	require.NoError(t, err)

	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var report Report
	require.NoError(t, result.DecodeSingleOutput(&report))
	require.Equal(t, StatusCompleted, report.Status)
	attempts := server.attemptTimes("/throttled")
	require.Len(t, attempts, 2, "one throttled attempt and one retry")
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), 900*time.Millisecond, "Dex honored Retry-After")
}

// TestAPIRequestExampleFailsAnUnwiredDefectWithRealDex shows the unwired optional defect branch failing the Flow.
func TestAPIRequestExampleFailsAnUnwiredDefectWithRealDex(t *testing.T) {
	server := newAuthorizationServer(t)
	flow, harness := newAPIRequestHarness(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "oauth2-http-api-request-defect-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	_, err := harness.client.StartFlow(ctx, flow, flowID, Input{Path: "userinfo-without-a-slash"}, dex.StartFlowOptions{})
	require.NoError(t, err)

	result := waitForTerminalFlow(t, ctx, harness.client, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, server.refreshCount(), "an invalid input stops before the token is touched")
}

func waitForTerminalFlow(t *testing.T, ctx context.Context, client *dex.Client, flowID string) dex.FlowResult {
	t.Helper()
	for {
		result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not reach a terminal status", flowID)
		return result
	}
}

// authorizationServer is a plain-http fake that serves the token endpoint and a small protected API.
type authorizationServer struct {
	*httptest.Server
	mutex           sync.Mutex
	refreshes       int
	validAccess     map[string]bool
	validRefresh    map[string]bool
	bearersByPath   map[string][]string
	attemptsByPath  map[string][]time.Time
	throttledOnce   bool
	issuedTokenPair int
}

func newAuthorizationServer(t *testing.T) *authorizationServer {
	t.Helper()
	server := &authorizationServer{
		validAccess: map[string]bool{initialAccessToken: true}, validRefresh: map[string]bool{initialRefreshToken: true},
		bearersByPath: map[string][]string{}, attemptsByPath: map[string][]time.Time{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", server.serveToken)
	mux.HandleFunc("/", server.serveAPI)
	server.Server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func (server *authorizationServer) serveToken(response http.ResponseWriter, request *http.Request) {
	_ = request.ParseForm()
	server.mutex.Lock()
	defer server.mutex.Unlock()
	refreshToken := request.PostForm.Get("refresh_token")
	response.Header().Set("Content-Type", "application/json")
	if request.PostForm.Get("client_secret") != "integration-client-secret" || !server.validRefresh[refreshToken] {
		response.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(response, `{"error":"invalid_grant"}`)
		return
	}
	delete(server.validRefresh, refreshToken)
	server.issuedTokenPair++
	access, refresh := "integration-access-token-"+strconv.Itoa(server.issuedTokenPair), "integration-refresh-token-"+strconv.Itoa(server.issuedTokenPair)
	server.validAccess[access], server.validRefresh[refresh] = true, true
	server.refreshes++
	_ = json.NewEncoder(response).Encode(map[string]any{"access_token": access, "refresh_token": refresh, "expires_in": 60, "token_type": "Bearer"})
}

func (server *authorizationServer) serveAPI(response http.ResponseWriter, request *http.Request) {
	authorization := request.Header.Get("Authorization")
	server.mutex.Lock()
	server.bearersByPath[request.URL.Path] = append(server.bearersByPath[request.URL.Path], authorization)
	server.attemptsByPath[request.URL.Path] = append(server.attemptsByPath[request.URL.Path], time.Now())
	isAuthorized := server.validAccess[strings.TrimPrefix(authorization, "Bearer ")]
	isFirstThrottledAttempt := request.URL.Path == "/throttled" && !server.throttledOnce
	if isFirstThrottledAttempt {
		server.throttledOnce = true
	}
	server.mutex.Unlock()
	switch {
	case !isAuthorized:
		response.WriteHeader(http.StatusUnauthorized)
	case isFirstThrottledAttempt:
		response.Header().Set("Retry-After", "1")
		response.WriteHeader(http.StatusTooManyRequests)
	case request.URL.Path == "/userinfo" || request.URL.Path == "/throttled":
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"sub":"user-1","preferred_username":"alice"}`)
	default:
		http.NotFound(response, request)
	}
}

func (server *authorizationServer) refreshCount() int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.refreshes
}

func (server *authorizationServer) bearerTokens(path string) []string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]string(nil), server.bearersByPath[path]...)
}

func (server *authorizationServer) attemptTimes(path string) []time.Time {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]time.Time(nil), server.attemptsByPath[path]...)
}

type apiRequestHarness struct {
	registry *dex.Registry
	cache    *blobcache.Cache
	worker   *dex.Worker
	result   chan error
	client   *dex.Client
}

// newAPIRequestHarness runs the Flow on a new Worker with a connection to server. The stored access token
// expired a minute before the harness starts.
func newAPIRequestHarness(t *testing.T, server *authorizationServer) (*Flow, *apiRequestHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "oauth2-http", Name: ConnectionName}
	expiresAt := time.Now().Add(-time.Minute)
	credentials := testsupport.NewRefreshingCredentials(oauth2http.Credentials{
		OAuthClientID: "integration-client", OAuthClientSecret: sdkgo.NewSecretString("integration-client-secret"),
		AccessToken: sdkgo.NewSecretString(initialAccessToken), RefreshToken: sdkgo.NewSecretString(initialRefreshToken),
	}, &expiresAt, nil)
	providerClient, err := oauth2http.New(oauth2http.Config{
		AuthorizationEndpoint: server.URL + "/authorize", TokenEndpoint: server.URL + "/token",
		Scopes: []string{"openid"}, APIBaseURL: server.URL,
	}, credentials)
	require.NoError(t, err)
	connection, err := oauth2http.NewConnection(providerClient, reference)
	require.NoError(t, err)
	return newAPIRequestHarnessWithConnection(t, connection)
}

// newAPIRequestHarnessWithConnection runs the Flow with one connection on a new Worker.
func newAPIRequestHarnessWithConnection(t *testing.T, connection oauth2http.Connection) (*Flow, *apiRequestHarness) {
	t.Helper()
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	harness := &apiRequestHarness{registry: registry, cache: cache, result: make(chan error, 1)}
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress,
	})
	require.NoError(t, err)
	go func() { harness.result <- harness.worker.Start() }()
	require.NoError(t, harness.worker.WaitPullRegistered(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.result, harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
