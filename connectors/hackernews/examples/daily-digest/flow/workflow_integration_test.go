//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dailydigest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hackernews"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDailyDigestWithRealDex(t *testing.T) {
	var feedCalls, storyCalls, summaryCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "stories.json") {
			if feedCalls.Add(1) == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(429)
				return
			}
			writeJSON(t, w, []int64{42, 43, 44, 42})
			return
		}
		switch r.URL.Path {
		case "/item/42.json":
			storyCalls.Add(1)
			writeJSON(t, w, hackernews.Item{ID: 42, Type: "story", Time: time.Now().Unix(), Title: "A compiler release", URL: "https://example.com/compiler", Kids: []int64{420}, Score: 100})
		case "/item/43.json":
			writeJSON(t, w, map[string]any{"id": 43, "deleted": true})
		case "/item/44.json":
			writeJSON(t, w, hackernews.Item{ID: 44, Type: "job", Time: time.Now().Unix(), Title: "Hiring"})
		case "/item/420.json":
			writeJSON(t, w, hackernews.Item{ID: 420, Type: "comment", Parent: 42, TextHTML: "<p>One build is faster.</p>"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	client, err := hackernews.New(hackernews.Config{BaseURL: provider.URL}, nil)
	require.NoError(t, err)
	connection, err := hackernews.NewConnection(client, sdkgo.ConnectionRef{Provider: "hacker-news", Name: ConnectionName})
	require.NoError(t, err)
	flow := &inspectionFlow{NewFlow(connection, func(_ context.Context, prompt string) ([]Selection, error) {
		summaryCalls.Add(1)
		require.Contains(t, prompt, `"language":"English"`)
		require.NotContains(t, prompt, "Hiring")
		return []Selection{{ID: 42, Summary: "The HN post announces a compiler release.", Why: "Developers can evaluate the new compiler.", Discussion: "One sampled commenter reports a faster build.", CommentIDs: []int64{420}}}, nil
	})}
	worker := startTestWorker(t, flow)
	t.Run("real retry, deduplicated reads, default English, source links", func(t *testing.T) {
		flowID := fmt.Sprintf("hn-once-%d", time.Now().UnixNano())
		_, err := worker.client.StartFlow(context.Background(), flow, flowID, Request{Once: true}, dex.StartFlowOptions{})
		require.NoError(t, err)
		result := waitForDigest(t, worker.client, flowID)
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var digest Digest
		require.NoError(t, result.DecodeSingleOutput(&digest))
		require.Equal(t, 1, digest.CandidateCount)
		require.Contains(t, digest.Markdown, "https://example.com/compiler")
		require.Contains(t, digest.Markdown, "item?id=420")
		require.Equal(t, int32(6), feedCalls.Load())
		require.Equal(t, int32(1), storyCalls.Load())
		var display map[string]any
		require.NoError(t, worker.client.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encoded, err := json.Marshal(display["hacker-news-daily-latest"])
		require.NoError(t, err)
		var displayed Digest
		require.NoError(t, json.Unmarshal(encoded, &displayed))
		require.Equal(t, digest, displayed)
	})
	t.Run("durable timer and worker replacement", func(t *testing.T) {
		flowID := fmt.Sprintf("hn-timer-%d", time.Now().UnixNano())
		deadline := time.Now().Add(15 * time.Second)
		_, err := worker.client.StartFlow(context.Background(), flow, flowID, Request{Once: true, FirstDigestAt: deadline.UTC().Format(time.RFC3339)}, dex.StartFlowOptions{})
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			var snapshot state
			err := worker.client.InvokeRPC(context.Background(), flowID, flow.GetTestState, nil, &snapshot)
			return err == nil && len(snapshot.Candidates) == 1 && snapshot.FeedIndex == 5 && len(snapshot.Pending) == 0
		}, 10*time.Second, 50*time.Millisecond)
		require.Equal(t, int32(1), summaryCalls.Load(), "the timer must not publish early")
		worker.restart(t)
		result := waitForDigest(t, worker.client, flowID)
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		require.Equal(t, int32(2), summaryCalls.Load(), "replacement must publish exactly one report")
	})
	t.Run("invalid input fails before any provider request", func(t *testing.T) {
		before := feedCalls.Load()
		flowID := fmt.Sprintf("hn-invalid-%d", time.Now().UnixNano())
		_, err := worker.client.StartFlow(context.Background(), flow, flowID, Request{Once: true, MaxItems: 11}, dex.StartFlowOptions{})
		require.NoError(t, err)
		require.Equal(t, dex.FlowFailed, waitForDigest(t, worker.client, flowID).Status)
		require.Equal(t, before, feedCalls.Load())
	})
}

type inspectionFlow struct{ *Flow }

func (flow *inspectionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil), dex.DefineRPC(flow.GetTestState, nil)}
}
func (flow *inspectionFlow) GetDexSummary(ctx dex.Context, input dex.None) (*dex.RPCResult[map[string]any], error) {
	return flow.Flow.GetDexSummary(ctx, input)
}
func (flow *inspectionFlow) GetDexDisplay(ctx dex.Context, input dex.None) (*dex.RPCResult[map[string]any], error) {
	return flow.Flow.GetDexDisplay(ctx, input)
}
func (*inspectionFlow) GetTestState(ctx dex.Context, _ dex.None) (*dex.RPCResult[state], error) {
	value, err := digestState.Get(ctx)
	return &dex.RPCResult[state]{Output: value}, err
}

type testWorker struct {
	client   *dex.Client
	worker   *dex.Worker
	registry *dex.Registry
	cache    *blobcache.Cache
	options  dex.WorkerOptions
	result   chan error
}

func startTestWorker(t *testing.T, flow dex.Flow) *testWorker {
	t.Helper()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	options := dex.WorkerOptions{BindAddress: address, FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS")}
	worker, err := dex.NewWorker(registry, cache, options)
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: options.FlowServiceAddress, WorkerTarget: &dex.WorkerTarget{Address: address}})
	require.NoError(t, err)
	suite := &testWorker{client: client, worker: worker, registry: registry, cache: cache, options: options, result: make(chan error, 1)}
	go func() { suite.result <- worker.Start() }()
	t.Cleanup(func() { suite.stop(t); require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	return suite
}
func (suite *testWorker) stop(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, suite.worker.Stop(ctx))
	require.NoError(t, <-suite.result)
}
func (suite *testWorker) restart(t *testing.T) {
	t.Helper()
	suite.stop(t)
	worker, err := dex.NewWorker(suite.registry, suite.cache, suite.options)
	require.NoError(t, err)
	suite.worker = worker
	go func() { suite.result <- worker.Start() }()
}
func waitForDigest(t *testing.T, client *dex.Client, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for {
		result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var timeout *dex.LongPollTimeoutError
		if errors.As(err, &timeout) {
			continue
		}
		require.NoError(t, err)
		return result
	}
}
func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
