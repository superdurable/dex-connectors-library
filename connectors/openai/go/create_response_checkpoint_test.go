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
	marker := createResponseCheckpoint{CallID: result.Receipt.CallID, Attempt: 1}
	require.Equal(t, marker, ctx.heartbeats[0], "the dispatch marker is the attempt's first heartbeat")
	for _, heartbeat := range ctx.heartbeats {
		require.NotNil(t, heartbeat, "a nil heartbeat clears the checkpoint")
	}
	require.Equal(t, createResponseCheckpoint{CallID: result.Receipt.CallID, Attempt: 1, ResponseID: "resp_quiet"}, ctx.heartbeats[len(ctx.heartbeats)-1])
}
