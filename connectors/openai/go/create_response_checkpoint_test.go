// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/openai/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// recordingContext keeps every heartbeat value and counts heartbeats after the attempt returned.
type recordingContext struct {
	*testsupport.DexContext
	mu             sync.Mutex
	heartbeats     []any
	hasReturned    atomic.Bool
	lateHeartbeats atomic.Int32
}

func (ctx *recordingContext) RecordHeartbeat(value any) error {
	if ctx.hasReturned.Load() {
		ctx.lateHeartbeats.Add(1)
	}
	ctx.mu.Lock()
	ctx.heartbeats = append(ctx.heartbeats, value)
	ctx.mu.Unlock()
	return ctx.DexContext.RecordHeartbeat(value)
}

func TestCreateResponseHeartbeatsTheCurrentCheckpointDuringAQuietCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = response.Write([]byte(`{"id":"resp_quiet","status":"completed"}`))
	}))
	defer server.Close()
	connection := sdkgo.ConnectionRef{Provider: "openai", Name: "default"}
	client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{connection: {APIKey: sdkgo.NewSecretString("test-key")}})
	require.NoError(t, err)
	client.checkpointHeartbeatInterval = 10 * time.Millisecond
	ctx := &recordingContext{DexContext: testsupport.NewDexContext("flow-1", "quiet")}
	result, err := sdkgo.RunMutation(ctx, client.CreateResponse(), connection, CreateRequest{Input: "profile"})
	ctx.hasReturned.Store(true)
	require.NoError(t, err)
	require.Equal(t, CreateResponseBranchCompleted, result.Branch)
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, ctx.lateHeartbeats.Load(), "Dex rejects heartbeats after Execute returns")

	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	require.Greater(t, len(ctx.heartbeats), 3, "the ticker beats while the provider is silent")
	oneSend := int32(1)
	marker := createResponseCheckpoint{CallID: result.Receipt.CallID, Attempt: 1, SendCount: &oneSend}
	require.Equal(t, marker, ctx.heartbeats[0], "the dispatch marker is the attempt's first heartbeat")
	for _, heartbeat := range ctx.heartbeats {
		require.NotNil(t, heartbeat, "a nil heartbeat clears the checkpoint")
	}
	require.Equal(t, createResponseCheckpoint{CallID: result.Receipt.CallID, Attempt: 1, ResponseID: "resp_quiet", SendCount: &oneSend}, ctx.heartbeats[len(ctx.heartbeats)-1])
}

func TestCreateResponseSendBudgetDerivesThePriorSendBoundFromTheCheckpoint(t *testing.T) {
	count := func(value int32) *int32 { return &value }
	for name, test := range map[string]struct {
		attempt   int32
		previous  createResponseCheckpoint
		isTrusted bool
		wantBound int32
	}{
		"first attempt":                  {attempt: 1, wantBound: 0},
		"no checkpoint":                  {attempt: 3, wantBound: 2},
		"checkpoint of an older release": {attempt: 3, previous: createResponseCheckpoint{Attempt: 2, CanResend: true}, isTrusted: true, wantBound: 2},
		"previous attempt sent nothing":  {attempt: 4, previous: createResponseCheckpoint{Attempt: 3, CanResend: true, SendCount: count(0)}, isTrusted: true, wantBound: 0},
		"skipped attempts may have sent": {attempt: 5, previous: createResponseCheckpoint{Attempt: 2, SendCount: count(1)}, isTrusted: true, wantBound: 3},
		"count larger than its attempt":  {attempt: 3, previous: createResponseCheckpoint{Attempt: 1, SendCount: count(2)}, isTrusted: true, wantBound: 2},
		"marker of this attempt":         {attempt: 2, previous: createResponseCheckpoint{Attempt: 2, SendCount: count(0)}, isTrusted: true, wantBound: 1},
		"untrusted checkpoint":           {attempt: 2, previous: createResponseCheckpoint{Attempt: 1, SendCount: count(0)}, wantBound: 1},
	} {
		t.Run(name, func(t *testing.T) {
			dexContext := testsupport.NewDexContext("flow-1", "budget")
			dexContext.AttemptNo = test.attempt
			budget := newCreateResponseSendBudget(sdkgo.Call{Context: dexContext}, test.previous, test.isTrusted)
			require.Equal(t, test.wantBound, budget.priorSendUpperBound)
		})
	}
}
