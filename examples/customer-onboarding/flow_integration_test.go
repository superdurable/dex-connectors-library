//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customeronboarding_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	customeronboarding "github.com/superdurable/dex-connectors-library/examples/customer-onboarding"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	mockprovider "github.com/superdurable/dex-connectors-library/test/mock-provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestQueryAndActionRunInRealDexFlow(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	connection := connector.ConnectionRef{Provider: "mock", Name: "default"}
	httpClient, err := httpconnector.New(httpconnector.Config{
		BaseURL: provider.URL(), CredentialHeaders: map[string]string{"api_key": "X-Mock-Api-Key"},
	}, connector.StaticCredentialProvider{connection: connector.NewCredential(map[string]string{"api_key": "test-key"})})
	require.NoError(t, err)
	flow := customeronboarding.NewCustomerOnboardingConnectorFlow(httpClient)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cacheDir := t.TempDir()
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(cacheDir, "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer cache.Close()
	port := availablePort(t)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: net.JoinHostPort("127.0.0.1", port), FlowServiceAddress: serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: net.JoinHostPort("127.0.0.1", port)},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: worker.WorkerTarget(),
	})
	require.NoError(t, err)
	defer client.Close()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "connector-acceptance-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, customeronboarding.Input{
		CustomerID: "customer-1", Connection: connection, Credits: 100,
	}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output customeronboarding.Output
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, connector.ActionSucceeded, output.Outcome)
	action, ok := provider.Action(string(output.CallID))
	require.True(t, ok)
	require.Equal(t, 100, action.Credits)
}

func availablePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
