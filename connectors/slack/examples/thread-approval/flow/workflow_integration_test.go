//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadapproval

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
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestThreadApprovalExampleCompletesOnceAndPreservesUncertainOutcomeWithRealDex(t *testing.T) {
	provider := newSlackProvider(t)
	defer provider.Close()
	flow, harness := newSlackIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	teamID := "T" + testRunID

	successRoot := slack.MessageEvent{
		TeamID: teamID, ChannelID: "C1", Timestamp: "1.0", ThreadTimestamp: "1.0", UserID: "U1", Text: "request approval",
	}
	successFlowID := startSlackThreadFlow(t, ctx, harness.client, flow, "Ev-root-success-"+testRunID, successRoot)
	waitForSlackStatus(t, ctx, harness.client, flow, successFlowID, StatusWaitingForReply)

	successReply := sdkgo.TriggerEvent[slack.MessageEvent]{
		ID: "Ev-reply-success-" + testRunID, OccurredAt: time.Unix(2, 0).UTC(),
		Payload: slack.MessageEvent{
			TeamID: teamID, ChannelID: "C1", Timestamp: "2.0", ThreadTimestamp: "1.0", UserID: "U2", Text: "approve",
		},
	}
	replyTarget := sdkgo.NewDexRPCTriggerTarget(harness.client, flow.ReceiveThreadReply, slack.FlowIDByThread(ResolveFlowID))
	require.NoError(t, replyTarget.HandleTrigger(ctx, successReply))
	require.NoError(t, replyTarget.HandleTrigger(ctx, successReply))

	result, err := harness.client.WaitForFlow(ctx, successFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var completedState ThreadState
	require.NoError(t, result.DecodeSingleOutput(&completedState))
	require.Equal(t, StatusCompleted, completedState.Status)
	require.Equal(t, "U2", completedState.ReplyUserID)
	require.Equal(t, 1, provider.postCount("1.0"))

	uncertainRoot := slack.MessageEvent{
		TeamID: teamID, ChannelID: "C1", Timestamp: "3.0", ThreadTimestamp: "3.0", UserID: "U1", Text: "request approval",
	}
	uncertainFlowID := startSlackThreadFlow(t, ctx, harness.client, flow, "Ev-root-uncertain-"+testRunID, uncertainRoot)
	waitForSlackStatus(t, ctx, harness.client, flow, uncertainFlowID, StatusWaitingForReply)
	uncertainReply := sdkgo.TriggerEvent[slack.MessageEvent]{
		ID: "Ev-reply-uncertain-" + testRunID, OccurredAt: time.Unix(4, 0).UTC(),
		Payload: slack.MessageEvent{
			TeamID: teamID, ChannelID: "C1", Timestamp: "4.0", ThreadTimestamp: "3.0", UserID: "U2", Text: "approve",
		},
	}
	require.NoError(t, replyTarget.HandleTrigger(ctx, uncertainReply))
	waitForSlackStatus(t, ctx, harness.client, flow, uncertainFlowID, StatusNeedsRecovery)
	require.NoError(t, replyTarget.HandleTrigger(ctx, uncertainReply))
	require.Equal(t, 1, provider.postCount("3.0"))
}

func startSlackThreadFlow(
	t *testing.T,
	ctx context.Context,
	client *dex.Client,
	flow *Flow,
	eventID string,
	payload slack.MessageEvent,
) string {
	t.Helper()
	startTarget := sdkgo.NewDexFlowTriggerTarget(client, flow, slack.FlowIDByThread(ResolveFlowID), BuildStartInput)
	event := sdkgo.TriggerEvent[slack.MessageEvent]{ID: eventID, OccurredAt: time.Now().UTC(), Payload: payload}
	require.NoError(t, startTarget.HandleTrigger(ctx, event))
	require.NoError(t, startTarget.HandleTrigger(ctx, event))
	flowID, err := ResolveFlowID(payload.ThreadIdentity())
	require.NoError(t, err)
	return flowID
}

func waitForSlackStatus(
	t *testing.T,
	ctx context.Context,
	client *dex.Client,
	flow *Flow,
	flowID string,
	expected Status,
) ThreadState {
	t.Helper()
	var state ThreadState
	require.Eventually(t, func() bool {
		state = ThreadState{}
		return client.InvokeRPC(ctx, flowID, flow.GetThreadStatus, nil, &state) == nil && state.Status == expected
	}, 20*time.Second, 100*time.Millisecond)
	return state
}

type slackProvider struct {
	*httptest.Server
	mutex         sync.Mutex
	postsByThread map[string]int
}

func newSlackProvider(t *testing.T) *slackProvider {
	t.Helper()
	provider := &slackProvider{postsByThread: map[string]int{}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	return provider
}

func (provider *slackProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/conversations.replies":
		threadTimestamp := request.URL.Query().Get("ts")
		response.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprintf(response, `{"ok":true,"messages":[{"ts":%q,"user":"U1","text":"request approval"}]}`, threadTimestamp)
		if err != nil {
			panic(err)
		}
	case "/chat.postMessage":
		var payload map[string]string
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			panic(err)
		}
		threadTimestamp := payload["thread_ts"]
		provider.mutex.Lock()
		provider.postsByThread[threadTimestamp]++
		provider.mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		if threadTimestamp == "3.0" {
			response.WriteHeader(http.StatusInternalServerError)
			writeSlackProviderResponse(response, `{"ok":false,"error":"fatal_error"}`)
			return
		}
		writeSlackProviderResponse(response, `{"ok":true,"channel":"C1","ts":"2.1","message":{"ts":"2.1","thread_ts":"1.0","user":"UBOT","text":"Processing complete"}}`)
	default:
		http.NotFound(response, request)
	}
}

func writeSlackProviderResponse(response http.ResponseWriter, body string) {
	if _, err := response.Write([]byte(body)); err != nil {
		panic(err)
	}
}

func (provider *slackProvider) postCount(threadTimestamp string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.postsByThread[threadTimestamp]
}

type slackIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newSlackIntegrationHarness(t *testing.T, endpoint string) (*Flow, *slackIntegrationHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "slack", Name: ConnectionName}
	providerClient, err := slack.New(slack.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[slack.Credentials]{
		reference: {
			BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"), AppToken: sdkgo.NewSecretString("app-token"),
		},
	})
	require.NoError(t, err)
	connection, err := slack.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableSlackIntegrationPort(t))
	harness := &slackIntegrationHarness{
		registry: registry, cache: cache, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"), workerAddress: workerAddress,
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if harness.worker != nil {
			harness.stopWorker(t)
		}
		require.NoError(t, errors.Join(harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func (harness *slackIntegrationHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
}

func (harness *slackIntegrationHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func availableSlackIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
