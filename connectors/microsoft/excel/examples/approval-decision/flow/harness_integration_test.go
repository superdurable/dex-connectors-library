//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvaldecision

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/internal/fakeexcel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAccessToken = "excel-integration-token"
	integrationDriveID     = "b!integrationDrive_01"
	policyWorkbookID       = "01POLICYWORKBOOK000000000000000001"
	summaryWorkbookID      = "01SUMMARYWORKBOOK00000000000000002"
	policyTableID          = "{6D182180-0000-4000-8000-0000000000a1}"
	decisionTableID        = "{6D182180-0000-4000-8000-0000000000a2}"
	summaryWorksheetID     = "{00000000-0001-0000-0000-000000000003}"
	// slowProviderDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowProviderDelay = 9 * time.Second
	// providerRequestTimeout lets a slow fake request finish inside the 30-second Execute timeout.
	providerRequestTimeout = 20 * time.Second
)

var decisionColumns = []string{"RequestId", "Requester", "Category", "AmountUsd", "Decision", "Approver", "DecidedAt"}

func newApprovalProvider(t *testing.T) *fakeexcel.Server {
	t.Helper()
	provider := fakeexcel.New(t, integrationAccessToken, "integration-refresh")
	provider.AddTable(integrationDriveID, policyWorkbookID, fakeexcel.Table{
		ID: policyTableID, Name: "ApprovalPolicy", Columns: []string{"Category", "MaxAutoApproveUsd", "Approver"},
		Rows: [][]any{{"travel", 500.0, "lead@contoso.com"}, {"hardware", 1500.0, "it@contoso.com"}},
	})
	provider.AddTable(integrationDriveID, policyWorkbookID, fakeexcel.Table{ID: decisionTableID, Name: "Decisions", Columns: decisionColumns})
	provider.AddWorksheet(integrationDriveID, summaryWorkbookID, summaryWorksheetID, "Summary")
	return provider
}

// decisionRows returns the decision log rows after the empty row every new Excel table starts with.
func decisionRows(provider *fakeexcel.Server) [][]any {
	return provider.TableRows(integrationDriveID, policyWorkbookID, "Decisions")[1:]
}

// approvalHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type approvalHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newApprovalHarness(t *testing.T, provider *fakeexcel.Server) *approvalHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "microsoft", Name: ConnectionName}
	providerClient, err := excel.New(excel.Config{}, sdkgo.StaticCredentialProvider[excel.Credentials]{reference: {
		AuthMethodID: excel.OAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(integrationAccessToken),
	}}, excel.WithLocalProviderURL(provider.URL), excel.WithHTTPClient(&http.Client{Timeout: providerRequestTimeout}))
	require.NoError(t, err)
	connection, err := excel.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness := &approvalHarness{
		flow: NewFlow(connection,
			sdkgo.ConnectorLoadedConfiguration[TableConfiguration]{Reference: PolicyTableConfigurationRef(), Value: TableConfiguration{
				DriveID: integrationDriveID, WorkbookID: policyWorkbookID, Table: policyTableID, TableName: "ApprovalPolicy"}},
			sdkgo.ConnectorLoadedConfiguration[TableConfiguration]{Reference: DecisionTableConfigurationRef(), Value: TableConfiguration{
				DriveID: integrationDriveID, WorkbookID: policyWorkbookID, Table: decisionTableID, TableName: "Decisions"}},
			sdkgo.ConnectorLoadedConfiguration[WorksheetConfiguration]{Reference: SummaryWorksheetConfigurationRef(), Value: WorksheetConfiguration{
				DriveID: integrationDriveID, WorkbookID: summaryWorkbookID, Worksheet: summaryWorksheetID, WorksheetName: "Summary"}},
		),
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
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

func (harness *approvalHarness) startWorker(t *testing.T) {
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
func (harness *approvalHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *approvalHarness) runDecision(t *testing.T, scenario string, input Input) ApprovalDecision {
	t.Helper()
	result := harness.waitForFlow(t, harness.startDecision(t, scenario, input))
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow failed: %s", result.ErrorMessage)
	var decision ApprovalDecision
	require.NoError(t, result.DecodeSingleOutput(&decision))
	return decision
}

func (harness *approvalHarness) startDecision(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "excel-approval-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func (harness *approvalHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	return result
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
