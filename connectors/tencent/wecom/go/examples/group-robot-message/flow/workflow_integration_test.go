//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package grouprobotmessage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go"
	"github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go/wecommock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationWebhookKey = "693a91f6-7a2c-4bc4-97a0-0ec2b1fa5aa0"

var integrationInput = wecom.SendGroupRobotMessageInput{MessageType: wecom.GroupRobotMessageMarkdown, Content: "**Deploy 42** finished"}

func TestGroupRobotMessageUsesDefaultMockWithRealDex(t *testing.T) {
	mock := wecommock.New(t, ConnectionName)
	harness := newGroupRobotHarness(t, mock.Connection())
	result := harness.waitForFlow(t, harness.startFlow(t, "mock"))
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output wecom.SentGroupRobotMessage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, wecom.GroupRobotMessageMarkdown, output.MessageType)
	require.Len(t, mock.SendGroupRobotMessage().Calls(), 1)
}

// A system-busy errcode proves WeCom did not process the post; the checkpoint lets the next real Dex attempt post.
func TestGroupRobotMessageRetriesSystemBusyWithRealDex(t *testing.T) {
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, integrationWebhookKey, r.URL.Query().Get("key"))
		body := `{"errcode":0,"errmsg":"ok"}`
		if requests.Add(1) == 1 {
			body = `{"errcode":-1,"errmsg":"system busy"}`
		}
		_, err := w.Write([]byte(body))
		require.NoError(t, err)
	}))
	t.Cleanup(provider.Close)
	harness := newGroupRobotHarness(t, newProviderConnection(t, provider.URL))
	result := harness.waitForFlow(t, harness.startFlow(t, "retry"))
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	require.EqualValues(t, 2, requests.Load())
}

// A dropped connection after WeCom read the post leaves its outcome unknown; the unwired uncertain branch fails the Flow.
func TestGroupRobotMessageNeverRepostsAnUnconfirmedPostWithRealDex(t *testing.T) {
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}))
	t.Cleanup(provider.Close)
	harness := newGroupRobotHarness(t, newProviderConnection(t, provider.URL))
	result := harness.waitForFlow(t, harness.startFlow(t, "uncertain"))
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "uncertain")
	require.NotContains(t, result.ErrorMessage, integrationWebhookKey)
	require.EqualValues(t, 1, requests.Load())
}

// WeCom accepts the post, then the Worker is lost before the Step commits. The attempt on the
// replacement Worker reads the durable claim heartbeat and must not post again.
func TestGroupRobotMessageNeverRepostsAfterTheWorkerIsLostWithRealDex(t *testing.T) {
	var requests atomic.Int32
	releaseHeldPost := make(chan struct{})
	var releaseOnce sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// The post is already accepted; the reply is withheld until the old Worker is gone.
		<-releaseHeldPost
		_, err := w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		require.NoError(t, err)
	}))
	t.Cleanup(provider.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseHeldPost) }) })
	harness := newGroupRobotHarness(t, newProviderConnection(t, provider.URL))
	flowID := harness.startFlow(t, "lost-worker")
	require.Eventually(t, func() bool { return requests.Load() == 1 }, 30*time.Second, 50*time.Millisecond, "the first attempt must reach WeCom")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	releaseOnce.Do(func() { close(releaseHeldPost) })
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "uncertain")
	require.NotContains(t, result.ErrorMessage, integrationWebhookKey)
	require.Contains(t, result.ErrorMessage, "an earlier attempt may have posted", "the replacement Worker's attempt read the claim checkpoint")
	require.EqualValues(t, 1, requests.Load(), "the attempt on the replacement Worker did not post again")
}

func newProviderConnection(t *testing.T, baseURL string) wecom.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "wecom", Name: ConnectionName}
	client, err := wecom.New(wecom.Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[wecom.Credentials]{
		reference: {AuthMethodID: "group-robot", WebhookKey: sdkgo.NewSecretString(integrationWebhookKey)},
	})
	require.NoError(t, err)
	connection, err := wecom.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

// groupRobotHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type groupRobotHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newGroupRobotHarness(t *testing.T, connection wecom.Connection) *groupRobotHarness {
	t.Helper()
	harness := &groupRobotHarness{flow: NewFlow(connection), serverAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS")}
	if harness.serverAddress == "" {
		harness.serverAddress = "127.0.0.1:8801"
	}
	var err error
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{FlowServiceAddress: harness.serverAddress})
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	harness.startWorker(t)
	return harness
}

func (harness *groupRobotHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{FlowServiceAddress: harness.serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	registrationContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(registrationContext))
	harness.worker, harness.workerResult = worker, workerResult
}

// replaceWorker force-stops the Worker without draining its handlers, like a crash, then starts a new one.
func (harness *groupRobotHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *groupRobotHarness) startFlow(t *testing.T, scenario string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "wecom-group-robot-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, integrationInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *groupRobotHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for {
		result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not close", flowID)
		return result
	}
}
