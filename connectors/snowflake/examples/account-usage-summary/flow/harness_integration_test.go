//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountusagesummary

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
	"github.com/superdurable/dex-connectors-library/connectors/snowflake"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake/internal/fakesnowflake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAccessToken = "integration-pat-0123456789"

// usageHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type usageHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newUsageProvider(t *testing.T, script func(fakesnowflake.SubmittedStatement) fakesnowflake.StatementScript) *fakesnowflake.Server {
	t.Helper()
	return fakesnowflake.New(t, fakesnowflake.Credentials{ProgrammaticAccessToken: integrationAccessToken}, script)
}

func newUsageHarness(t *testing.T, provider *fakesnowflake.Server, maximumRunningReads int) *usageHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "snowflake", Name: ConnectionName}
	providerClient, err := snowflake.New(
		snowflake.Config{AccountIdentifier: "myorg-analytics", Warehouse: "REPORTING_WH", Database: "ANALYTICS", Schema: "BILLING"},
		sdkgo.StaticCredentialProvider[snowflake.Credentials]{reference: {
			AuthMethodID: snowflake.ProgrammaticAccessTokenAuthMethodID, ProgrammaticAccessToken: sdkgo.NewSecretString(integrationAccessToken),
		}},
		snowflake.WithLocalProviderURL(provider.URL),
	)
	require.NoError(t, err)
	connection, err := snowflake.NewConnection(providerClient, reference)
	require.NoError(t, err)
	policy := StatusPolicy{CheckInterval: time.Second, MaximumRunningReads: maximumRunningReads}
	harness := &usageHarness{flow: NewFlow(connection, &policy), serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.startWorker(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

func (harness *usageHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	harness.worker, harness.workerResult = worker, workerResult
}

// replaceWorker force-stops the Worker without draining its handlers, like a crash, then starts a new one.
func (harness *usageHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *usageHarness) startSummary(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "snowflake-usage-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	requestID := "start-" + flowID
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{RequestID: &requestID})
	require.NoError(t, err)
	return flowID
}

func (harness *usageHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for {
		result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var timeout *dex.LongPollTimeoutError
		if errors.As(err, &timeout) {
			continue
		}
		require.NoError(t, err)
		return result
	}
}

// runSummary starts a Flow, waits for it, and checks that the record RPC agrees with the Flow result.
func (harness *usageHarness) runSummary(t *testing.T, scenario string, input Input) UsageSummaryRecord {
	t.Helper()
	flowID := harness.startSummary(t, scenario, input)
	return harness.requireCompletedRecord(t, flowID)
}

func (harness *usageHarness) requireCompletedRecord(t *testing.T, flowID string) UsageSummaryRecord {
	t.Helper()
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s failed: %s", flowID, result.ErrorMessage)
	var record UsageSummaryRecord
	require.NoError(t, result.DecodeSingleOutput(&record))
	var stored UsageSummaryRecord
	require.NoError(t, harness.client.InvokeRPC(context.Background(), flowID, harness.flow.GetUsageSummaryRecord, nil, &stored))
	require.Equal(t, record, stored, "the record Attribute matches the Flow result")
	return record
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
