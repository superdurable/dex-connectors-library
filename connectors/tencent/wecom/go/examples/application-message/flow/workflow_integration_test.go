//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package applicationmessage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go"
	"github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go/wecommock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationCorpSecret = "fake-integration-secret_0123456789abcdefghij"

var integrationInput = wecom.SendApplicationMessageInput{
	ToUserIDs: []string{"zhangsan"}, ToDepartmentIDs: []string{"2"}, MessageType: wecom.ApplicationMessageText, Content: "Incident INC-7 is open",
}

func TestApplicationMessageUsesDefaultMockWithRealDex(t *testing.T) {
	mock := wecommock.New(t, ConnectionName)
	result := runApplicationFlow(t, mock.Connection(), "mock")
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output wecom.SentApplicationMessage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.NotEmpty(t, output.MessageID)
	require.Len(t, mock.SendApplicationMessage().Calls(), 1)
}

// A 500 after WeCom read the send is safe to retry: the identical request is suppressed by WeCom's duplicate check.
func TestApplicationMessageRetriesAnUnconfirmedSendWithIdenticalBytesWithRealDex(t *testing.T) {
	provider := &fakeApplicationProvider{t: t}
	server := httptest.NewServer(provider)
	defer server.Close()
	result := runApplicationFlow(t, newProviderConnection(t, server.URL), "retry")
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output wecom.SentApplicationMessage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "fake-msgid-2", output.MessageID)
	require.Equal(t, []string{"wangwu"}, output.InvalidUserIDs)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Equal(t, 1, provider.tokenRequests, "the cached token serves the retry")
	require.Len(t, provider.sendBodies, 2)
	require.Equal(t, provider.sendBodies[0], provider.sendBodies[1])
	require.Contains(t, provider.sendBodies[0], `"enable_duplicate_check":1`)
}

// fakeApplicationProvider issues one token, answers the first send with 500, then accepts.
type fakeApplicationProvider struct {
	t             *testing.T
	mutex         sync.Mutex
	tokenRequests int
	sendBodies    []string
}

func (provider *fakeApplicationProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	switch r.URL.Path {
	case "/cgi-bin/gettoken":
		require.Equal(provider.t, integrationCorpSecret, r.URL.Query().Get("corpsecret"))
		provider.tokenRequests++
		_, err := w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"fake-integration-token","expires_in":7200}`))
		require.NoError(provider.t, err)
	case "/cgi-bin/message/send":
		require.Equal(provider.t, "fake-integration-token", r.URL.Query().Get("access_token"))
		body, err := io.ReadAll(r.Body)
		require.NoError(provider.t, err)
		provider.sendBodies = append(provider.sendBodies, string(body))
		if len(provider.sendBodies) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, err = w.Write([]byte(`{"errcode":0,"errmsg":"ok","invaliduser":"wangwu","msgid":"fake-msgid-2"}`))
		require.NoError(provider.t, err)
	default:
		http.NotFound(w, r)
	}
}

func newProviderConnection(t *testing.T, baseURL string) wecom.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "wecom", Name: ConnectionName}
	client, err := wecom.New(wecom.Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[wecom.Credentials]{
		reference: {AuthMethodID: "application", CorpID: "ww1a2b3c4d5e6f7a8b", AgentID: "1000002", CorpSecret: sdkgo.NewSecretString(integrationCorpSecret)},
	})
	require.NoError(t, err)
	connection, err := wecom.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func runApplicationFlow(t *testing.T, connection wecom.Connection, scenario string) dex.FlowResult {
	t.Helper()
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "wecom-application-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, integrationInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	return result
}
