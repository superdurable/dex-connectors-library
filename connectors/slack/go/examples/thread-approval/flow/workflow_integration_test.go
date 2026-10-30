//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadapproval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestThreadApprovalExampleRetriesSafePostsAndNeverResendsUnknownPostsWithRealDex(t *testing.T) {
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
	replyFilter, err := NewReplyTriggerFilter(slack.ThreadReplyCreatedTriggerConfiguration{
		ChannelID: "C1", ThreadReplyMatcher: slack.MessageMatcher{MessageContains: "approve", PosterUserIDs: []string{"U2"}},
	})
	require.NoError(t, err)
	replyTarget := sdkgo.NewDexRPCTriggerTarget(
		harness.client, flow.ReceiveThreadReply, replyFilter, ResolveFlowID, MapToReceiveThreadReplyInput,
	)
	rejectedReply := successReply
	rejectedReply.ID = "Ev-reply-rejected-" + testRunID
	rejectedReply.Payload.UserID = "U3"
	require.NoError(t, replyTarget.HandleTrigger(ctx, rejectedReply))
	require.Equal(t, StatusWaitingForReply, waitForSlackStatus(t, ctx, harness.client, flow, successFlowID, StatusWaitingForReply).Status)
	require.NoError(t, replyTarget.HandleTrigger(ctx, successReply))
	require.Eventually(t, func() bool {
		err = replyTarget.HandleTrigger(ctx, successReply)
		return err == nil || !strings.Contains(err.Error(), "attribute keys are locked")
	}, 20*time.Second, 50*time.Millisecond)

	result, err := harness.client.WaitForFlow(ctx, successFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "flow error: %s; post count: %d", result.ErrorMessage, provider.postCount("1.0"))
	var completedState ThreadState
	require.NoError(t, result.DecodeSingleOutput(&completedState))
	require.Equal(t, StatusCompleted, completedState.Status)
	require.Equal(t, "U2", completedState.ReplyUserID)
	require.Equal(t, 1, provider.postCount("1.0"))
	require.Equal(t, "Integration complete.", provider.postText("1.0"))

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
	uncertainResult, err := harness.client.WaitForFlow(ctx, uncertainFlowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, uncertainResult.Status)
	require.Equal(t, 1, provider.postCount("3.0"))

	acceptedRoot := slack.MessageEvent{
		TeamID: teamID, ChannelID: "C1", Timestamp: "5.0", ThreadTimestamp: "5.0", UserID: "U1", Text: "request approval",
	}
	acceptedFlowID := startSlackThreadFlow(t, ctx, harness.client, flow, "Ev-root-accepted-"+testRunID, acceptedRoot)
	waitForSlackStatus(t, ctx, harness.client, flow, acceptedFlowID, StatusWaitingForReply)
	acceptedReply := sdkgo.TriggerEvent[slack.MessageEvent]{
		ID: "Ev-reply-accepted-" + testRunID, OccurredAt: time.Unix(6, 0).UTC(),
		Payload: slack.MessageEvent{
			TeamID: teamID, ChannelID: "C1", Timestamp: "6.0", ThreadTimestamp: "5.0", UserID: "U2", Text: "approve",
		},
	}
	require.NoError(t, replyTarget.HandleTrigger(ctx, acceptedReply))
	acceptedResult, err := harness.client.WaitForFlow(ctx, acceptedFlowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, acceptedResult.Status)
	require.Equal(t, 1, provider.postCount("5.0"))

	retryRoot := slack.MessageEvent{
		TeamID: teamID, ChannelID: "C1", Timestamp: "7.0", ThreadTimestamp: "7.0", UserID: "U1", Text: "request approval",
	}
	retryFlowID := startSlackThreadFlow(t, ctx, harness.client, flow, "Ev-root-retry-"+testRunID, retryRoot)
	waitForSlackStatus(t, ctx, harness.client, flow, retryFlowID, StatusWaitingForReply)
	retryReply := sdkgo.TriggerEvent[slack.MessageEvent]{
		ID: "Ev-reply-retry-" + testRunID, OccurredAt: time.Unix(8, 0).UTC(),
		Payload: slack.MessageEvent{
			TeamID: teamID, ChannelID: "C1", Timestamp: "8.0", ThreadTimestamp: "7.0", UserID: "U2", Text: "approve",
		},
	}
	require.NoError(t, replyTarget.HandleTrigger(ctx, retryReply))
	retryResult, err := harness.client.WaitForFlow(ctx, retryFlowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, retryResult.Status, "flow error: %s", retryResult.ErrorMessage)
	require.Equal(t, 2, provider.postCount("7.0"))
	messageIDs := provider.postMessageIDs("7.0")
	require.Len(t, messageIDs, 2)
	require.NotEmpty(t, messageIDs[0])
	require.Equal(t, messageIDs[0], messageIDs[1])

	gatewayRoot := slack.MessageEvent{
		TeamID: teamID, ChannelID: "C1", Timestamp: "9.0", ThreadTimestamp: "9.0", UserID: "U1", Text: "request approval",
	}
	gatewayFlowID := startSlackThreadFlow(t, ctx, harness.client, flow, "Ev-root-gateway-"+testRunID, gatewayRoot)
	waitForSlackStatus(t, ctx, harness.client, flow, gatewayFlowID, StatusWaitingForReply)
	gatewayReply := sdkgo.TriggerEvent[slack.MessageEvent]{
		ID: "Ev-reply-gateway-" + testRunID, OccurredAt: time.Unix(10, 0).UTC(),
		Payload: slack.MessageEvent{
			TeamID: teamID, ChannelID: "C1", Timestamp: "10.0", ThreadTimestamp: "9.0", UserID: "U2", Text: "approve",
		},
	}
	require.NoError(t, replyTarget.HandleTrigger(ctx, gatewayReply))
	gatewayResult, err := harness.client.WaitForFlow(ctx, gatewayFlowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, gatewayResult.Status)
	require.Equal(t, 1, provider.postCount("9.0"))

	malformedRoot := slack.MessageEvent{
		TeamID: teamID, ChannelID: "C1", Timestamp: "11.0", ThreadTimestamp: "11.0", UserID: "U1", Text: "request approval",
	}
	malformedFlowID := startSlackThreadFlow(t, ctx, harness.client, flow, "Ev-root-malformed-"+testRunID, malformedRoot)
	waitForSlackStatus(t, ctx, harness.client, flow, malformedFlowID, StatusWaitingForReply)
	malformedReply := sdkgo.TriggerEvent[slack.MessageEvent]{
		ID: "Ev-reply-malformed-" + testRunID, OccurredAt: time.Unix(12, 0).UTC(),
		Payload: slack.MessageEvent{
			TeamID: teamID, ChannelID: "C1", Timestamp: "12.0", ThreadTimestamp: "11.0", UserID: "U2", Text: "approve",
		},
	}
	require.NoError(t, replyTarget.HandleTrigger(ctx, malformedReply))
	malformedResult, err := harness.client.WaitForFlow(ctx, malformedFlowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, malformedResult.Status)
	require.Contains(t, malformedResult.ErrorMessage, "uncertain")
	require.Equal(t, 1, provider.postCount("11.0"))
}

type slackIntegrationCredentialSource struct {
	postCredentials slack.Credentials
}

func (source slackIntegrationCredentialSource) Resolve(call sdkgo.Call) (slack.Credentials, error) {
	if call.Operation.OperationID == "postThreadReply" {
		return source.postCredentials, nil
	}
	return slack.Credentials{BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token")}, nil
}

func (source slackIntegrationCredentialSource) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[slack.Credentials]) (slack.Credentials, error) {
	return source.Resolve(call)
}

func TestThreadApprovalExampleClassifiesTerminalPostCredentialsWithRealDex(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		threadTimestamp string
		postCredentials slack.Credentials
		wantFailure     string
		wantPosts       int
	}{
		{name: "before_post", threadTimestamp: "13.0", postCredentials: slack.Credentials{UserToken: sdkgo.NewSecretString("user-token")}, wantFailure: "defect"},
		{name: "during_read_back", threadTimestamp: "15.0", postCredentials: slack.Credentials{BotToken: sdkgo.NewSecretString("bot-token")}, wantFailure: "uncertain", wantPosts: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newSlackProvider(t)
			defer provider.Close()
			reference := sdkgo.ConnectionRef{Provider: "slack", Name: ConnectionName}
			providerClient, err := slack.New(slack.Config{Endpoint: provider.URL}, slackIntegrationCredentialSource{postCredentials: testCase.postCredentials})
			require.NoError(t, err)
			connection, err := slack.NewConnection(providerClient, reference)
			require.NoError(t, err)
			flow, harness := newSlackIntegrationHarnessWithConnection(t, connection)
			harness.startWorker(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
			root := slack.MessageEvent{TeamID: "T" + testRunID, ChannelID: "C1", Timestamp: testCase.threadTimestamp, ThreadTimestamp: testCase.threadTimestamp, UserID: "U1", Text: "request approval"}
			flowID := startSlackThreadFlow(t, ctx, harness.client, flow, "Ev-root-credentials-"+testRunID, root)
			waitForSlackStatus(t, ctx, harness.client, flow, flowID, StatusWaitingForReply)
			reply := sdkgo.TriggerEvent[slack.MessageEvent]{
				ID: "Ev-reply-credentials-" + testRunID, OccurredAt: time.Now().UTC(),
				Payload: slack.MessageEvent{TeamID: root.TeamID, ChannelID: "C1", Timestamp: "16.0", ThreadTimestamp: testCase.threadTimestamp, UserID: "U2", Text: "approve"},
			}
			replyFilter, err := NewReplyTriggerFilter(slack.ThreadReplyCreatedTriggerConfiguration{ChannelID: "C1", ThreadReplyMatcher: slack.MessageMatcher{MessageContains: "approve", PosterUserIDs: []string{"U2"}}})
			require.NoError(t, err)
			replyTarget := sdkgo.NewDexRPCTriggerTarget(harness.client, flow.ReceiveThreadReply, replyFilter, ResolveFlowID, MapToReceiveThreadReplyInput)
			require.NoError(t, replyTarget.HandleTrigger(ctx, reply))
			result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
			require.NoError(t, err)
			require.Equal(t, dex.FlowFailed, result.Status)
			require.Contains(t, result.ErrorMessage, testCase.wantFailure)
			require.Equal(t, testCase.wantPosts, provider.postCount(testCase.threadTimestamp))
		})
	}
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
	startFilter, err := NewStartTriggerFilter(slack.ChannelThreadCreatedTriggerConfiguration{
		ChannelID: "C1", ThreadTriggerMatcher: slack.MessageMatcher{MessageContains: "request approval", PosterUserIDs: []string{"U1"}},
	})
	require.NoError(t, err)
	startTarget := sdkgo.NewDexFlowTriggerTarget(client, flow, startFilter, ResolveFlowID, MapToFlowInput)
	event := sdkgo.TriggerEvent[slack.MessageEvent]{ID: eventID, OccurredAt: time.Now().UTC(), Payload: payload}
	require.NoError(t, startTarget.HandleTrigger(ctx, event))
	require.NoError(t, startTarget.HandleTrigger(ctx, event))
	return ResolveFlowID(event)
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
	mutex              sync.Mutex
	postsByThread      map[string]int
	textByThread       map[string]string
	messageIDsByThread map[string][]string
}

func newSlackProvider(t *testing.T) *slackProvider {
	t.Helper()
	provider := &slackProvider{postsByThread: map[string]int{}, textByThread: map[string]string{}, messageIDsByThread: map[string][]string{}}
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
		provider.textByThread[threadTimestamp] = payload["text"]
		provider.messageIDsByThread[threadTimestamp] = append(provider.messageIDsByThread[threadTimestamp], payload["client_msg_id"])
		postCount := provider.postsByThread[threadTimestamp]
		provider.mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		if threadTimestamp == "3.0" {
			response.WriteHeader(http.StatusInternalServerError)
			writeSlackProviderResponse(response, `{"ok":false,"error":"fatal_error"}`)
			return
		}
		if threadTimestamp == "5.0" {
			writeSlackProviderResponse(response, `{"ok":true,"message":{"text":"Integration complete."}}`)
			return
		}
		if threadTimestamp == "7.0" && postCount == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			writeSlackProviderResponse(response, `{"ok":false,"error":"service_unavailable"}`)
			return
		}
		if threadTimestamp == "9.0" {
			response.WriteHeader(http.StatusGatewayTimeout)
			writeSlackProviderResponse(response, `{"ok":false,"error":"service_unavailable"}`)
			return
		}
		if threadTimestamp == "11.0" {
			writeSlackProviderResponse(response, `{"ok":false,"error":"token_expired","ts":123}`)
			return
		}
		if threadTimestamp == "15.0" {
			writeSlackProviderResponse(response, `{"ok":true,"ts":"15.1"}`)
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

func (provider *slackProvider) postText(threadTimestamp string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.textByThread[threadTimestamp]
}

func (provider *slackProvider) postMessageIDs(threadTimestamp string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.messageIDsByThread[threadTimestamp]...)
}

type slackIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
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
	return newSlackIntegrationHarnessWithConnection(t, connection)
}

func newSlackIntegrationHarnessWithConnection(t *testing.T, connection slack.Connection) (*Flow, *slackIntegrationHarness) {
	t.Helper()
	flow := NewFlow(connection, sdkgo.ConnectorLoadedConfiguration[PostCompletionConfiguration]{
		Reference: PostCompletionConfigurationRef(), Value: PostCompletionConfiguration{Text: "Integration complete."},
	})
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness := &slackIntegrationHarness{
		registry: registry, cache: cache, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress,
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
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{FlowServiceAddress: harness.serverAddress})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
}

func (harness *slackIntegrationHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
