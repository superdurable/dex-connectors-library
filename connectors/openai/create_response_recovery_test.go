// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/connectors/openai/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// lossyContext drops every heartbeat, like a checkpoint Dex never persisted
// or the local phase of async durability.
type lossyContext struct {
	*testsupport.DexContext
	isDropping bool
}

func (ctx *lossyContext) RecordHeartbeat(value any) error {
	if ctx.isDropping {
		return nil
	}
	return ctx.DexContext.RecordHeartbeat(value)
}

// failingHeartbeatContext accepts the first heartbeat of an attempt and fails every later one.
type failingHeartbeatContext struct {
	*testsupport.DexContext
	heartbeats atomic.Int32
}

func (ctx *failingHeartbeatContext) RecordHeartbeat(value any) error {
	if ctx.heartbeats.Add(1) > 1 {
		return errors.New("heartbeat stream is closed")
	}
	return ctx.DexContext.RecordHeartbeat(value)
}

// providerScript serves one scripted reply per request and records each request's method.
type providerScript struct {
	t       *testing.T
	mu      sync.Mutex
	methods []string
	replies []func(http.ResponseWriter, *http.Request)
}

func newProviderScript(t *testing.T, replies ...func(http.ResponseWriter, *http.Request)) (*providerScript, *httptest.Server) {
	script := &providerScript{t: t, replies: replies}
	server := httptest.NewServer(http.HandlerFunc(script.serve))
	t.Cleanup(server.Close)
	return script, server
}

func (script *providerScript) serve(response http.ResponseWriter, request *http.Request) {
	_, _ = io.ReadAll(request.Body)
	script.mu.Lock()
	index := len(script.methods)
	script.methods = append(script.methods, request.Method)
	var reply func(http.ResponseWriter, *http.Request)
	if index < len(script.replies) {
		reply = script.replies[index]
	}
	script.mu.Unlock()
	if reply == nil {
		script.t.Errorf("unexpected request %d: %s %s", index+1, request.Method, request.URL.Path)
		response.WriteHeader(http.StatusTeapot)
		return
	}
	reply(response, request)
}

func (script *providerScript) requestMethods() []string {
	script.mu.Lock()
	defer script.mu.Unlock()
	return append([]string(nil), script.methods...)
}

func replyStatus(status int, header ...string) func(http.ResponseWriter, *http.Request) {
	return func(response http.ResponseWriter, _ *http.Request) {
		for index := 0; index+1 < len(header); index += 2 {
			response.Header().Set(header[index], header[index+1])
		}
		response.WriteHeader(status)
	}
}

func replyJSON(body string) func(http.ResponseWriter, *http.Request) {
	return func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(body))
	}
}

// replyDroppedConnection reads the whole request and closes the connection without answering.
func replyDroppedConnection(t *testing.T) func(http.ResponseWriter, *http.Request) {
	return func(response http.ResponseWriter, _ *http.Request) {
		connection, _, err := response.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack connection: %v", err)
			return
		}
		_ = connection.Close()
	}
}

func runCreateResponse(ctx dex.Context, client *openai.Client, options ...sdkgo.RunOption) (openai.CreateResponseResult, error) {
	return sdkgo.RunMutation(ctx, client.CreateResponse(), openAIConnection, openai.CreateRequest{Input: "profile"}, options...)
}

// A 429 leaves an idle keep-alive connection; attempt 2 reuses it, and the provider drops it after the POST.
func TestCreateResponseNeverReplaysAPostThatMayHaveReachedOpenAI(t *testing.T) {
	script, server := newProviderScript(t,
		replyStatus(http.StatusTooManyRequests),
		replyDroppedConnection(t),
		replyJSON(`{"id":"resp_second","status":"completed"}`),
	)
	client := newClient(t, server.URL)
	ctx := testsupport.NewDexContext("flow-1", "replay")
	_, err := runCreateResponse(ctx, client)
	require.ErrorContains(t, err, "429")
	ctx.AttemptNo = 2
	result, err := runCreateResponse(ctx, client)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, []string{http.MethodPost, http.MethodPost}, script.requestMethods(), "net/http must not send the create a third time")
}

func TestCreateResponseLostDispatchCheckpointNeverResends(t *testing.T) {
	script, server := newProviderScript(t, replyDroppedConnection(t))
	client := newClient(t, server.URL)
	ctx := &lossyContext{DexContext: testsupport.NewDexContext("flow-1", "lost"), isDropping: true}
	result, err := runCreateResponse(ctx, client)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
	ctx.isDropping = false
	ctx.AttemptNo = 2
	result, err = runCreateResponse(ctx, client)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
	require.Contains(t, result.Failure.Message, "sync Execute durability")
	require.Equal(t, []string{http.MethodPost}, script.requestMethods())
}

// Async durability runs its local attempts without heartbeat checkpoints, so a
// retried attempt selects uncertain instead of sending again.
func TestCreateResponseFailsClosedWithoutRestoredCheckpoints(t *testing.T) {
	script, server := newProviderScript(t, replyStatus(http.StatusTooManyRequests))
	client := newClient(t, server.URL)
	ctx := &lossyContext{DexContext: testsupport.NewDexContext("flow-1", "async"), isDropping: true}
	_, err := runCreateResponse(ctx, client)
	require.ErrorContains(t, err, "429")
	ctx.AttemptNo = 2
	result, err := runCreateResponse(ctx, client)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
	require.Equal(t, []string{http.MethodPost}, script.requestMethods())
}

func TestCreateResponseResendsOnlyAfterTheImmediatelyPreviousAttemptProvedNoResponse(t *testing.T) {
	t.Run("next attempt sends again", func(t *testing.T) {
		script, server := newProviderScript(t, replyStatus(http.StatusServiceUnavailable), replyJSON(`{"id":"resp_retry","status":"completed"}`))
		client := newClient(t, server.URL)
		ctx := testsupport.NewDexContext("flow-1", "resend")
		_, err := runCreateResponse(ctx, client)
		require.ErrorContains(t, err, "503")
		ctx.AttemptNo = 2
		result, err := runCreateResponse(ctx, client)
		require.NoError(t, err)
		require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch)
		require.Equal(t, []string{http.MethodPost, http.MethodPost}, script.requestMethods())
	})
	t.Run("a skipped attempt may have sent", func(t *testing.T) {
		script, server := newProviderScript(t, replyStatus(http.StatusTooManyRequests))
		client := newClient(t, server.URL)
		ctx := testsupport.NewDexContext("flow-1", "stale")
		_, err := runCreateResponse(ctx, client)
		require.ErrorContains(t, err, "429")
		ctx.AttemptNo = 3
		result, err := runCreateResponse(ctx, client)
		require.NoError(t, err)
		require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
		require.Equal(t, []string{http.MethodPost}, script.requestMethods())
	})
}

func TestCreateResponseClassifiesNonStreamingTerminalStatuses(t *testing.T) {
	for _, test := range []struct {
		status     string
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
	}{
		{status: "completed", wantBranch: openai.CreateResponseBranchCompleted},
		{status: "failed", wantBranch: openai.CreateResponseBranchFailed, wantKind: sdkgo.FailureProviderRejection},
		{status: "incomplete", wantBranch: openai.CreateResponseBranchFailed, wantKind: sdkgo.FailureProviderRejection},
		{status: "", wantBranch: openai.CreateResponseBranchUncertain, wantKind: sdkgo.FailureProtocol},
		{status: "requires_review", wantBranch: openai.CreateResponseBranchUncertain, wantKind: sdkgo.FailureProtocol},
	} {
		t.Run(cmpStatusName(test.status), func(t *testing.T) {
			script, server := newProviderScript(t, replyJSON(`{"id":"resp_status","status":"`+test.status+`"}`))
			result, err := runCreateResponse(testsupport.NewDexContext("flow-1", "status-"+test.status), newClient(t, server.URL))
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, "resp_status", result.Value.ID)
			if test.wantKind != "" {
				require.Equal(t, test.wantKind, result.Failure.Kind)
			}
			require.Equal(t, []string{http.MethodPost}, script.requestMethods(), "a terminal or unrecognized status is never polled")
		})
	}
}

func cmpStatusName(status string) string {
	if status == "" {
		return "empty"
	}
	return status
}

// OpenAI cancels a synchronous response when its connection closes, so a new create is safe.
func TestCreateResponseCreatesAgainAfterACancelledResponse(t *testing.T) {
	script, server := newProviderScript(t,
		replyJSON(`{"id":"resp_cancelled","status":"in_progress"}`),
		replyJSON(`{"id":"resp_cancelled","status":"cancelled"}`),
		replyJSON(`{"id":"resp_new","status":"completed"}`),
	)
	client := newClient(t, server.URL)
	ctx := testsupport.NewDexContext("flow-1", "cancelled")
	_, err := runCreateResponse(ctx, client)
	require.ErrorContains(t, err, "cancelled")
	ctx.AttemptNo = 2
	result, err := runCreateResponse(ctx, client)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch)
	require.Equal(t, "resp_new", result.Value.ID)
	require.Equal(t, []string{http.MethodPost, http.MethodGet, http.MethodPost}, script.requestMethods())
}

func TestCreateResponseReadBackClassifiesUnconfirmableOutcomes(t *testing.T) {
	for _, test := range []struct {
		name        string
		readBack    func(http.ResponseWriter, *http.Request)
		wantKind    sdkgo.FailureKind
		wantMessage string
	}{
		{name: "not found", readBack: replyStatus(http.StatusNotFound), wantKind: sdkgo.FailureNotFound, wantMessage: "cannot be confirmed"},
		{name: "unauthorized", readBack: replyStatus(http.StatusUnauthorized), wantKind: sdkgo.FailureAuthentication, wantMessage: "GET /v1/responses/{id}"},
		{name: "forbidden", readBack: replyStatus(http.StatusForbidden), wantKind: sdkgo.FailureAuthorization, wantMessage: "read permission for responses"},
		{name: "another response", readBack: replyJSON(`{"id":"resp_other","status":"completed"}`), wantKind: sdkgo.FailureProtocol, wantMessage: "another response ID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			script, server := newProviderScript(t, replyJSON(`{"id":"resp_pending","status":"queued"}`), test.readBack)
			result, err := runCreateResponse(testsupport.NewDexContext("flow-1", "read-back-"+test.name), newClient(t, server.URL))
			require.NoError(t, err)
			require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.wantMessage)
			require.Equal(t, "resp_pending", result.Receipt.ProviderObjectID)
			require.Equal(t, []string{http.MethodPost, http.MethodGet}, script.requestMethods())
		})
	}
}

func TestCreateResponseReadBackWaitsOutTransientFailures(t *testing.T) {
	script, server := newProviderScript(t,
		replyJSON(`{"id":"resp_pending","status":"in_progress"}`),
		replyStatus(http.StatusTooManyRequests, "Retry-After", "1"),
		replyStatus(http.StatusBadGateway),
		replyJSON(`{"id":"resp_pending","status":"completed"}`),
	)
	result, err := runCreateResponse(testsupport.NewDexContext("flow-1", "read-back-transient"), newClient(t, server.URL))
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch)
	require.Equal(t, []string{http.MethodPost, http.MethodGet, http.MethodGet, http.MethodGet}, script.requestMethods())
}

func TestCreateResponseReadBackRetriesBeforeTheAttemptDeadlineAndResumesByReading(t *testing.T) {
	var reads atomic.Int32
	script, server := newProviderScript(t, replyJSON(`{"id":"resp_slow","status":"in_progress"}`))
	pending := func(response http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		_, _ = response.Write([]byte(`{"id":"resp_slow","status":"in_progress"}`))
	}
	for range 20 {
		script.replies = append(script.replies, pending)
	}
	client := newClient(t, server.URL)
	ctx := testsupport.NewDexContext("flow-1", "read-back-deadline")
	attemptContext, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	ctx.Context = attemptContext
	started := time.Now()
	_, err := runCreateResponse(ctx, client)
	require.ErrorContains(t, err, "not confirmed yet")
	require.Less(t, time.Since(started), 5*time.Second, "read-back returns Retry before the attempt's deadline")
	require.Positive(t, reads.Load())

	script.mu.Lock()
	script.replies = append(script.replies[:len(script.methods)], replyJSON(`{"id":"resp_slow","status":"completed"}`))
	script.mu.Unlock()
	ctx.Context = context.Background()
	ctx.AttemptNo = 2
	result, err := runCreateResponse(ctx, client)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch)
	methods := script.requestMethods()
	require.Equal(t, http.MethodPost, methods[0])
	for _, method := range methods[1:] {
		require.Equal(t, http.MethodGet, method, "the next attempt reads the checkpointed response instead of creating another")
	}
}

func TestCreateResponseKeepsAResultWhenLaterHeartbeatsFail(t *testing.T) {
	t.Run("non-streaming", func(t *testing.T) {
		_, server := newProviderScript(t, replyJSON(`{"id":"resp_kept","status":"completed"}`))
		ctx := &failingHeartbeatContext{DexContext: testsupport.NewDexContext("flow-1", "heartbeat-failure")}
		result, err := runCreateResponse(ctx, newClient(t, server.URL))
		require.NoError(t, err)
		require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch)
		require.Equal(t, "resp_kept", result.Value.ID)
	})
	t.Run("streaming", func(t *testing.T) {
		_, server := newProviderScript(t, replyEventStream(
			`{"type":"response.created","response":{"id":"resp_kept","status":"in_progress"}}`,
			`{"type":"response.completed","response":{"id":"resp_kept","status":"completed"}}`,
		))
		ctx := &failingHeartbeatContext{DexContext: testsupport.NewDexContext("flow-1", "stream-heartbeat-failure")}
		result, err := runCreateResponse(ctx, newClient(t, server.URL), sdkgo.WithProgressStream(dex.DefineStream[sdkgo.ProgressUpdate]("openai-progress", 1<<10)))
		require.NoError(t, err)
		require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch)
		require.Equal(t, "resp_kept", result.Value.ID)
	})
}

// Progress writes fail outside a Dex Worker; the connector still reads the stream to its end.
func TestCreateResponseKeepsReadingAfterProgressDeliveryFails(t *testing.T) {
	script, server := newProviderScript(t, replyEventStream(
		`{"type":"response.created","response":{"id":"resp_streamed","status":"in_progress"}}`,
		`{"type":"response.in_progress","response":{"id":"resp_streamed","status":"in_progress"}}`,
		`{"type":"response.output_text.delta","delta":"kept"}`,
		`{"type":"response.completed","response":{"id":"resp_streamed","status":"completed","output":[{"content":[{"type":"output_text","text":"kept"}]}]}}`,
	))
	result, err := runCreateResponse(testsupport.NewDexContext("flow-1", "progress-failure"), newClient(t, server.URL),
		sdkgo.WithProgressStream(dex.DefineStream[sdkgo.ProgressUpdate]("openai-progress", 1<<10)))
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch)
	require.Equal(t, "kept", result.Value.OutputText)
	require.Equal(t, []string{http.MethodPost}, script.requestMethods(), "the completed event decides the outcome without a read-back")
}

func replyEventStream(events ...string) func(http.ResponseWriter, *http.Request) {
	return func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: "+strings.Join(events, "\n\ndata: ")+"\n\n")
	}
}

// A 408 reply names no response and does not prove that none was stored.
func TestCreateResponseRequestTimeoutIsUncertainAndNeverResent(t *testing.T) {
	requireUncertainWithoutResend(t, http.StatusRequestTimeout, sdkgo.FailureTransport)
}

// OpenAI offers no create key or response lookup that could resolve a 409.
func TestCreateResponseConflictIsUncertainAndNeverResent(t *testing.T) {
	requireUncertainWithoutResend(t, http.StatusConflict, sdkgo.FailureConflict)
}

// Only 503 promises the request was not processed; other 5xx replies may follow a stored response.
func TestCreateResponseServerFailuresOtherThan503AreUncertainAndNeverResent(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusGatewayTimeout} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			requireUncertainWithoutResend(t, status, sdkgo.FailureAvailability)
		})
	}
}

// requireUncertainWithoutResend also runs attempt 2, which sends again only after a recorded resend permission.
func requireUncertainWithoutResend(t *testing.T, status int, wantKind sdkgo.FailureKind) {
	t.Helper()
	script, server := newProviderScript(t, replyStatus(status, "X-Request-Id", "req_ambiguous"))
	client := newClient(t, server.URL)
	ctx := testsupport.NewDexContext("flow-1", fmt.Sprintf("ambiguous-%d", status))
	result, err := runCreateResponse(ctx, client)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
	require.Equal(t, wantKind, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, fmt.Sprint(status))
	require.Equal(t, "req_ambiguous", result.Receipt.ProviderRequestID)
	ctx.AttemptNo = 2
	result, err = runCreateResponse(ctx, client)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
	require.Equal(t, []string{http.MethodPost}, script.requestMethods())
}

func TestCreateResponseHonorsBothRetryAfterForms(t *testing.T) {
	for _, test := range []struct {
		name       string
		retryAfter func() string
		wantMin    time.Duration
		wantMax    time.Duration
	}{
		{name: "delay-seconds", retryAfter: func() string { return "7" }, wantMin: 7 * time.Second, wantMax: 7 * time.Second},
		{name: "HTTP-date", retryAfter: func() string { return time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat) }, wantMin: 88 * time.Second, wantMax: 90 * time.Second},
		{name: "delay-seconds over one hour", retryAfter: func() string { return "99999999999999999999" }, wantMin: time.Hour, wantMax: time.Hour},
		{name: "HTTP-date over one hour", retryAfter: func() string { return time.Now().Add(1000 * time.Hour).UTC().Format(http.TimeFormat) }, wantMin: time.Hour, wantMax: time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, server := newProviderScript(t, func(response http.ResponseWriter, request *http.Request) {
				replyStatus(http.StatusTooManyRequests, "Retry-After", test.retryAfter())(response, request)
			})
			_, err := runCreateResponse(testsupport.NewDexContext("flow-1", "retry-after-"+test.name), newClient(t, server.URL))
			var retryAfter *dex.RetryAfterError
			require.ErrorAs(t, err, &retryAfter)
			require.GreaterOrEqual(t, retryAfter.After, test.wantMin)
			require.LessOrEqual(t, retryAfter.After, test.wantMax)
		})
	}
	for _, value := range []string{"0", "soon", time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat)} {
		t.Run("ignored "+value, func(t *testing.T) {
			_, server := newProviderScript(t, replyStatus(http.StatusTooManyRequests, "Retry-After", value))
			_, err := runCreateResponse(testsupport.NewDexContext("flow-1", "retry-after-ignored"), newClient(t, server.URL))
			require.ErrorContains(t, err, "429")
			var retryAfter *dex.RetryAfterError
			require.False(t, errors.As(err, &retryAfter), "the Step's retry policy applies")
		})
	}
}
