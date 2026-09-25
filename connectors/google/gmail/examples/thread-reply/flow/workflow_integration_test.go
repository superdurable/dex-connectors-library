//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadreply

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
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
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestThreadReplyExampleCompletesOnceAndPreservesUncertainOutcomeWithRealDex(t *testing.T) {
	provider := newGmailProvider(t)
	defer provider.Close()
	flow, harness := newGmailIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	successRoot := gmail.MessageEvent{
		PrimaryEmail: "owner@example.com", MessageID: "root-success", ThreadID: "thread-success",
		From: "sender@example.com", Subject: "Approval request", Snippet: "request approval",
	}
	successFlowID := startGmailThreadFlow(t, ctx, harness.client, flow, "root-success", successRoot)
	waitForGmailStatus(t, ctx, harness.client, flow, successFlowID, StatusWaitingForReply)

	successReply := connector.TriggerEvent[gmail.MessageEvent]{
		ID: "reply-success", OccurredAt: time.Unix(2, 0).UTC(),
		Payload: gmail.MessageEvent{
			PrimaryEmail: "owner@example.com", MessageID: "reply-success", ThreadID: "thread-success",
			From: "sender@example.com", Subject: "Re: Approval request", Snippet: "approved", IsReply: true,
		},
	}
	replyTarget := connector.NewDexRPCTriggerTarget(harness.client, flow.ReplyTriggerRPC().Definition(), gmail.FlowIDByThread(ResolveFlowID))
	require.NoError(t, replyTarget.HandleTrigger(ctx, successReply))
	require.NoError(t, replyTarget.HandleTrigger(ctx, successReply))

	result, err := harness.client.WaitForFlow(ctx, successFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var completedState ThreadState
	require.NoError(t, result.DecodeSingleOutput(&completedState))
	require.Equal(t, StatusCompleted, completedState.Status)
	require.Equal(t, "reply-success", completedState.ReplyMessageID)
	require.Equal(t, 1, provider.sendCount("thread-success"))

	uncertainRoot := gmail.MessageEvent{
		PrimaryEmail: "owner@example.com", MessageID: "root-uncertain", ThreadID: "thread-uncertain",
		From: "sender@example.com", Subject: "Approval request", Snippet: "request approval",
	}
	uncertainFlowID := startGmailThreadFlow(t, ctx, harness.client, flow, "root-uncertain", uncertainRoot)
	waitForGmailStatus(t, ctx, harness.client, flow, uncertainFlowID, StatusWaitingForReply)
	uncertainReply := connector.TriggerEvent[gmail.MessageEvent]{
		ID: "reply-uncertain", OccurredAt: time.Unix(4, 0).UTC(),
		Payload: gmail.MessageEvent{
			PrimaryEmail: "owner@example.com", MessageID: "reply-uncertain", ThreadID: "thread-uncertain",
			From: "sender@example.com", Subject: "Re: Approval request", Snippet: "approved", IsReply: true,
		},
	}
	require.NoError(t, replyTarget.HandleTrigger(ctx, uncertainReply))
	waitForGmailStatus(t, ctx, harness.client, flow, uncertainFlowID, StatusNeedsRecovery)
	require.NoError(t, replyTarget.HandleTrigger(ctx, uncertainReply))
	require.Equal(t, 1, provider.sendCount("thread-uncertain"))
}

func startGmailThreadFlow(
	t *testing.T,
	ctx context.Context,
	client *dex.Client,
	flow *Flow,
	eventID string,
	payload gmail.MessageEvent,
) string {
	t.Helper()
	startTarget := connector.NewDexFlowTriggerTarget(client, flow, gmail.FlowIDByThread(ResolveFlowID), BuildStartInput)
	event := connector.TriggerEvent[gmail.MessageEvent]{ID: eventID, OccurredAt: time.Now().UTC(), Payload: payload}
	require.NoError(t, startTarget.HandleTrigger(ctx, event))
	require.NoError(t, startTarget.HandleTrigger(ctx, event))
	flowID, err := ResolveFlowID(payload.ThreadIdentity())
	require.NoError(t, err)
	return flowID
}

func waitForGmailStatus(
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

type gmailProvider struct {
	*httptest.Server
	mutex         sync.Mutex
	sendsByThread map[string]int
}

func newGmailProvider(t *testing.T) *gmailProvider {
	t.Helper()
	provider := &gmailProvider{sendsByThread: map[string]int{}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	return provider
}

func (provider *gmailProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/users/me/messages/"):
		messageID := strings.TrimPrefix(request.URL.Path, "/users/me/messages/")
		threadID := "thread-success"
		if strings.Contains(messageID, "uncertain") {
			threadID = "thread-uncertain"
		}
		isReply := strings.HasPrefix(messageID, "reply-")
		response.Header().Set("Content-Type", "application/json")
		writeGmailProviderResponse(response, gmailIntegrationMessageJSON(messageID, threadID, isReply))
	case request.Method == http.MethodPost && request.URL.Path == "/users/me/messages/send":
		var payload map[string]string
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			panic(err)
		}
		threadID := payload["threadId"]
		provider.mutex.Lock()
		provider.sendsByThread[threadID]++
		provider.mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		if threadID == "thread-uncertain" {
			response.WriteHeader(http.StatusInternalServerError)
			writeGmailProviderResponse(response, `{"error":{"message":"unavailable"}}`)
			return
		}
		writeGmailProviderResponse(response, `{"id":"sent-success","threadId":"thread-success"}`)
	default:
		http.NotFound(response, request)
	}
}

func writeGmailProviderResponse(response http.ResponseWriter, body string) {
	if _, err := response.Write([]byte(body)); err != nil {
		panic(err)
	}
}

func (provider *gmailProvider) sendCount(threadID string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.sendsByThread[threadID]
}

func gmailIntegrationMessageJSON(messageID string, threadID string, isReply bool) string {
	headers := []map[string]string{
		{"name": "Message-ID", "value": "<" + messageID + "@example.com>"},
		{"name": "From", "value": "Sender <sender@example.com>"},
		{"name": "To", "value": "owner@example.com"},
		{"name": "Subject", "value": "Approval request"},
	}
	if isReply {
		headers = append(headers,
			map[string]string{"name": "In-Reply-To", "value": "<root@example.com>"},
			map[string]string{"name": "References", "value": "<root@example.com>"},
		)
	}
	contents, err := json.Marshal(map[string]any{
		"id": messageID, "threadId": threadID, "labelIds": []string{"INBOX"},
		"snippet": "approval granted", "internalDate": "1000",
		"payload": map[string]any{
			"mimeType": "text/plain", "headers": headers,
			"body": map[string]string{"data": base64.RawURLEncoding.EncodeToString([]byte("approval granted"))},
		},
	})
	if err != nil {
		panic(err)
	}
	return string(contents)
}

type gmailIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newGmailIntegrationHarness(t *testing.T, endpoint string) (*Flow, *gmailIntegrationHarness) {
	t.Helper()
	reference := connector.ConnectionRef{Provider: "gmail", Name: ConnectionName}
	providerClient, err := gmail.New(gmail.Config{Endpoint: endpoint}, connector.StaticCredentialProvider[gmail.Credentials]{
		reference: {AccessToken: connector.NewSecretString("gmail-token"), PrimaryEmail: "owner@example.com"},
	})
	require.NoError(t, err)
	connection, err := gmail.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableGmailIntegrationPort(t))
	harness := &gmailIntegrationHarness{
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

func (harness *gmailIntegrationHarness) startWorker(t *testing.T) {
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

func (harness *gmailIntegrationHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func availableGmailIntegrationPort(t *testing.T) string {
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
