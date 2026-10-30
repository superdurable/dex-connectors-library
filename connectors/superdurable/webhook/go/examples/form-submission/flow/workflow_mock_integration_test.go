//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package formsubmission

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook/go"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook/go/webhookmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFormSubmissionExampleRunsOnWebhookMockWithRealDex(t *testing.T) {
	mock := webhookmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()

	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
	}()
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := FlowIDPrefix + "mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	submission := Submission{
		EventID: "evt_mock_1", ContentType: webhook.ContentTypeJSON,
		ReceivedAt: time.Date(2026, time.October, 26, 12, 0, 0, 0, time.UTC),
		JSONBody:   json.RawMessage(`{"email":"ada@example.test"}`),
	}
	_, err = client.StartFlow(ctx, flow, flowID, submission, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome ForwardingOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, webhook.SendEventBranchDelivered, outcome.Branch)
	require.Equal(t, 202, outcome.StatusCode)
	require.NotEmpty(t, outcome.WebhookID)
	calls := mock.SendEvent().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, webhook.SendEventBranchDelivered, calls[0].Branch)
	require.JSONEq(t, `{"type":"form.submitted","submissionId":"evt_mock_1","receivedAt":"2026-10-26T12:00:00Z","jsonBody":{"email":"ada@example.test"}}`, string(calls[0].Input.Payload))
}
