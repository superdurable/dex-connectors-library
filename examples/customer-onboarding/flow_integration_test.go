//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customeronboarding_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
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

func TestQueryAndMutationRetriesUseRealDexIdentity(t *testing.T) {
	provider := mockprovider.StartWithOptions(mockprovider.Options{ProfileFailures: 1, MutationRateLimits: 1})
	defer provider.Close()
	connection, httpClient := connectorClient(t, provider)
	flow := customeronboarding.NewCustomerOnboardingConnectorFlow(httpClient)
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	output := runCustomerOnboarding(t, harness.client, flow, uniqueFlowID("retry"), customeronboarding.Input{
		CustomerID: "customer-1", Connection: connection, Credits: 100,
	})
	require.Equal(t, connector.MutationSucceeded, output.Outcome)
	require.Equal(t, 2, provider.ProfileRequests())
	require.Equal(t, 2, provider.MutationAttempts(output.CallID))
	require.Equal(t, 1, provider.MutationCount(output.CallID))
	mutation, ok := provider.Mutation(string(output.CallID))
	require.True(t, ok)
	require.Equal(t, 100, mutation.Credits)
}

func TestUnknownMutationUsesExplicitRecoveryAndCommittedReceipt(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	connection, httpClient := connectorClient(t, provider)
	flow := customeronboarding.NewCustomerOnboardingConnectorFlow(httpClient)
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	output := runCustomerOnboarding(t, harness.client, flow, uniqueFlowID("unknown"), customeronboarding.Input{
		CustomerID: "customer-2", Connection: connection, Credits: 200, SimulateUnknown: true,
	})
	require.Equal(t, connector.MutationSucceeded, output.Outcome)
	require.Equal(t, 1, provider.MutationCount(output.CallID))
	require.GreaterOrEqual(t, provider.RecoveryRequests(), 1)
}

func TestFlowContinuesAfterWorkerRestart(t *testing.T) {
	provider := mockprovider.StartWithOptions(mockprovider.Options{RecoveryFailures: 1})
	defer provider.Close()
	connection, httpClient := connectorClient(t, provider)
	flow := customeronboarding.NewCustomerOnboardingConnectorFlow(httpClient)
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := uniqueFlowID("restart")
	_, err := harness.client.StartFlow(ctx, flow, flowID, customeronboarding.Input{
		CustomerID: "customer-3", Connection: connection, Credits: 300, SimulateUnknown: true,
	}, dex.StartFlowOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return provider.RecoveryRequests() >= 1 }, 20*time.Second, 20*time.Millisecond)
	harness.stopWorker(t)
	harness.startWorker(t)

	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output customeronboarding.Output
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, connector.MutationSucceeded, output.Outcome)
	require.Equal(t, 1, provider.MutationCount(output.CallID))
}

func TestFlowRPCCannotCallProvider(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	connection, httpClient := connectorClient(t, provider)
	flow := &rpcBoundaryFlow{query: httpClient.Query()}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := uniqueFlowID("rpc-boundary")
	_, err := harness.client.StartFlow(ctx, flow, flowID, struct{}{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	var output bool
	err = harness.client.InvokeRPC(ctx, flowID, flow.AttemptProviderQuery, connection, &output)
	require.ErrorContains(t, err, "RPC invocation is not allowed")
	require.Zero(t, provider.ProfileRequests())
}

type rpcBoundaryFlow struct {
	dex.FlowDefaults
	query httpconnector.QueryOperation
}

func (flow *rpcBoundaryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(rpcBoundaryStartStep{})}
}

func (flow *rpcBoundaryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.AttemptProviderQuery, nil)}
}

func (*rpcBoundaryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

func (flow *rpcBoundaryFlow) AttemptProviderQuery(ctx dex.Context, connection connector.ConnectionRef) (*dex.RPCResult[bool], error) {
	_, err := connector.RunQuery(ctx, flow.query, connection, httpconnector.Request{
		Method: http.MethodGet, Path: "/profiles/customer-rpc",
	})
	return &dex.RPCResult[bool]{Output: err == nil}, err
}

type rpcBoundaryStartStep struct {
	dex.StepDefaultsNoWaitFor[struct{}]
}

func (rpcBoundaryStartStep) Execute(dex.Context, struct{}) (*dex.StepDecision, error) {
	return dex.DeadEnd(), nil
}

type dexHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newDexHarness(t *testing.T, flows []dex.Flow) *dexHarness {
	t.Helper()
	registry, err := dex.NewRegistry(flows)
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	address := net.JoinHostPort("127.0.0.1", availablePort(t))
	harness := &dexHarness{
		registry: registry, cache: cache,
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
		workerAddress: address,
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress,
		WorkerTarget:       &dex.WorkerTarget{Address: address},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if harness.worker != nil {
			harness.stopWorker(t)
		}
		require.NoError(t, errors.Join(harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

func (harness *dexHarness) startWorker(t *testing.T) {
	t.Helper()
	require.Nil(t, harness.worker)
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
}

func (harness *dexHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func connectorClient(t *testing.T, provider *mockprovider.Provider) (connector.ConnectionRef, *httpconnector.Client) {
	t.Helper()
	connection := connector.ConnectionRef{Provider: "mock", Name: "default"}
	client, err := httpconnector.New(httpconnector.Config{
		BaseURL: provider.URL(), CredentialHeaders: map[string]string{"api_key": "X-Mock-Api-Key"},
	}, connector.StaticCredentialProvider{connection: connector.NewCredential(map[string]string{"api_key": "test-key"})})
	require.NoError(t, err)
	return connection, client
}

func runCustomerOnboarding(
	t *testing.T,
	client *dex.Client,
	flow *customeronboarding.CustomerOnboardingConnectorFlow,
	flowID string,
	input customeronboarding.Input,
) customeronboarding.Output {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output customeronboarding.Output
	require.NoError(t, result.DecodeSingleOutput(&output))
	return output
}

func uniqueFlowID(prefix string) string {
	return fmt.Sprintf("connector-%s-%d", prefix, time.Now().UnixNano())
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
