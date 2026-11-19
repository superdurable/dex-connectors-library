//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package repositoryevent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSignedRepositoryWebhookStartsFlowWithRealDex(t *testing.T) {
	secret := "bitbucket-webhook-integration-secret"
	reference := sdkgo.ConnectionRef{Provider: "bitbucket", Name: ConnectionName}
	providerClient, err := bitbucket.New(bitbucket.Config{}, sdkgo.StaticCredentialProvider[bitbucket.Credentials]{reference: {
		AccessToken: sdkgo.NewSecretString("test-token"),
	}}, bitbucket.WithWebhookSecret(sdkgo.NewSecretString(secret)))
	require.NoError(t, err)
	connection, err := bitbucket.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow()
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
	ctx, cancel := context.WithCancel(context.Background())
	target := sdkgo.NewDexFlowTriggerTarget(client, flow, AcceptEveryEvent, ResolveFlowID, MapToFlowInput)
	trigger := bitbucket.NewRepositoryEventReceivedTrigger(bitbucket.RepositoryEventReceivedTriggerConfig{
		Connection: connection, ConnectionName: ConnectionName, BindingName: TriggerBindingName,
		Configuration: bitbucket.RepositoryEventReceivedTriggerConfiguration{}, Target: target,
	})
	handler, err := connection.RepositoryEventReceivedWebhookHandler()
	require.NoError(t, err)
	runner, err := webhooktrigger.NewEndpointRunner(handler, trigger)
	require.NoError(t, err)
	triggerResult := make(chan error, 1)
	go func() { triggerResult <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-triggerResult, context.Canceled)
		stopContext, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult, client.Close(), cache.Close()))
	})
	registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
	require.NoError(t, worker.WaitPullRegistered(registrationContext))
	cancelRegistration()
	requestUUID := "d4d90346-68af-4d66-82d4-" + strconv.FormatInt(time.Now().UnixNano()%1000000000000, 10)
	body := `{"repository":{"uuid":"{a1}","full_name":"meridian/api"},"pullrequest":{"id":7}}`
	mac := hmac.New(sha256.New, []byte(secret))
	_, err = mac.Write([]byte(body))
	require.NoError(t, err)
	deliver := func(signature string) int {
		request := httptest.NewRequest(http.MethodPost, "/bitbucket/events", strings.NewReader(body))
		request.Header.Set("X-Hub-Signature", signature)
		request.Header.Set("X-Event-Key", bitbucket.PullRequestCreatedEventKey)
		request.Header.Set("X-Request-UUID", requestUUID)
		response := httptest.NewRecorder()
		runner.ServeHTTP(response, request)
		return response.Code
	}
	require.Eventually(t, func() bool { return deliver("sha256=bad") == http.StatusBadRequest }, 10*time.Second, 20*time.Millisecond)
	require.Equal(t, http.StatusOK, deliver("sha256="+hex.EncodeToString(mac.Sum(nil))))
	waitContext, cancelWait := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelWait()
	flowID := ResolveFlowID(sdkgo.TriggerEvent[bitbucket.RepositoryEvent]{ID: requestUUID + ":" + bitbucket.PullRequestCreatedEventKey})
	var result dex.FlowResult
	require.Eventually(t, func() bool {
		var waitErr error
		result, waitErr = client.WaitForFlow(waitContext, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		return waitErr == nil
	}, 30*time.Second, 20*time.Millisecond)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output bitbucket.RepositoryEvent
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, int64(7), output.PullRequestID)
	require.Equal(t, "meridian/api", output.RepositoryFullName)
}
