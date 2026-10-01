//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountlifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

type accountLifecycleHarness struct {
	cache        *blobcache.Cache
	worker       *dex.Worker
	workerResult chan error
	client       *dex.Client
}

func onboardingInput() Input {
	return Input{
		Action: AccountActionOnboard, UserPrincipalName: "ada.lovelace@contoso.com", GivenName: "Ada", Surname: "Lovelace",
		Department: "Engineering", UsageLocation: "GB", GroupID: testGroupID,
	}
}

func newAccountLifecycleHarness(t *testing.T, graph *graphfake.Server) (*Flow, *accountLifecycleHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "microsoft", Name: ConnectionName}
	providerClient, err := entraid.New(entraid.Config{}, sdkgo.StaticCredentialProvider[entraid.Credentials]{
		reference: {AuthMethodID: entraid.EntraAppOnlyAuthMethodID, AccessToken: sdkgo.NewSecretString(graphfake.AccessToken)},
	}, entraid.WithLocalProviderURL(graph.URL))
	require.NoError(t, err)
	connection, err := entraid.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &accountLifecycleHarness{cache: cache}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- harness.worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func startAccountLifecycle(t *testing.T, ctx context.Context, client *dex.Client, flow *Flow, scenario string, input Input) string {
	t.Helper()
	flowID := "entra-account-lifecycle-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func waitForCompletedHandOff(t *testing.T, ctx context.Context, client *dex.Client, flowID string) AccountHandOff {
	t.Helper()
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", flowID, result.ErrorMessage)
	var handOff AccountHandOff
	require.NoError(t, result.DecodeSingleOutput(&handOff))
	return handOff
}

func waitForFailedFlow(t *testing.T, ctx context.Context, client *dex.Client, flowID string) {
	t.Helper()
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status)
}

// requireNoPasswordInFlow reads every Attribute through the display RPC and checks that no generated password is present.
func requireNoPasswordInFlow(t *testing.T, ctx context.Context, client *dex.Client, flow *Flow, flowID string, graph *graphfake.Server) {
	t.Helper()
	var display map[string]any
	require.NoError(t, client.InvokeRPC(ctx, flowID, flow.GetDexDisplay, dex.None(nil), &display))
	encoded, err := json.Marshal(display)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "entra-account-hand-off")
	passwords := graph.Passwords()
	require.NotEmpty(t, passwords)
	for _, password := range passwords {
		require.Len(t, password, 44)
		require.NotContains(t, string(encoded), password)
	}
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func availableIntegrationPort(t *testing.T) string {
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
