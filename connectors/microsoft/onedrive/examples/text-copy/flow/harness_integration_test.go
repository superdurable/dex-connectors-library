//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textcopy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationToken = "onedrive-integration-token"
	sourceDriveID    = "b!sourceLibrary_1"
	destinationDrive = "b!financeLibrary-2"
	sourceText       = "Ops Policy\nRefunds above 500 need approval.\n"
)

// graphFixture is the fake tenant: a Policies folder in the source library and a Reports folder in the destination library.
type graphFixture struct {
	*graphfake.Server
	policiesFolderID string
	reportsFolderID  string
}

func newGraphFixture(t *testing.T) *graphFixture {
	t.Helper()
	server := graphfake.NewServer(graphfake.Config{AccessToken: integrationToken, MyDriveID: sourceDriveID, DriveIDs: []string{sourceDriveID, destinationDrive}})
	t.Cleanup(server.Close)
	fixture := &graphFixture{Server: server}
	fixture.policiesFolderID = server.AddItem(graphfake.Item{Name: "Policies", DriveID: sourceDriveID, ParentID: "root", IsFolder: true})
	fixture.reportsFolderID = server.AddItem(graphfake.Item{Name: "Reports", DriveID: destinationDrive, ParentID: "root", IsFolder: true})
	server.AddItem(graphfake.Item{Name: "Ops Policy.txt", DriveID: sourceDriveID, ParentID: fixture.policiesFolderID, MimeType: "text/plain", Content: []byte(sourceText)})
	// A same-named decoy outside the source folder must never be read.
	server.AddItem(graphfake.Item{Name: "Ops Policy.txt", DriveID: sourceDriveID, ParentID: "root", MimeType: "text/plain", Content: []byte("2025 draft")})
	return fixture
}

type textCopyRun struct {
	status       dex.FlowStatus
	errorMessage string
	outcome      Outcome
}

type textCopyHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

// newTextCopyHarness starts a Worker for the example Flow wired to the fixture's folders on a real Dex Server.
func newTextCopyHarness(t *testing.T, fixture *graphFixture) *textCopyHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "microsoft", Name: ConnectionName}
	providerClient, err := onedrive.New(onedrive.Config{Endpoint: fixture.URL}, sdkgo.StaticCredentialProvider[onedrive.Credentials]{
		reference: {AuthMethodID: onedrive.MicrosoftOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(integrationToken)},
	})
	require.NoError(t, err)
	connection, err := onedrive.NewConnection(providerClient, reference)
	require.NoError(t, err)
	return newTextCopyHarnessWithConnection(t, connection, fixture.policiesFolderID, fixture.reportsFolderID)
}

func newTextCopyHarnessWithConnection(t *testing.T, connection onedrive.Connection, sourceFolderID, destinationFolderID string) *textCopyHarness {
	t.Helper()
	flow := NewFlow(connection,
		sdkgo.ConnectorLoadedConfiguration[LocationConfiguration]{Reference: SourceLocationConfigurationRef(), Value: LocationConfiguration{DriveID: sourceDriveID, FolderID: sourceFolderID}},
		sdkgo.ConnectorLoadedConfiguration[LocationConfiguration]{Reference: DestinationLocationConfigurationRef(), Value: LocationConfiguration{DriveID: destinationDrive, FolderID: destinationFolderID}},
	)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness := &textCopyHarness{
		flow: flow, registry: registry, cache: cache, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress,
	})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: harness.serverAddress})
	require.NoError(t, err)
	harness.worker, harness.workerResult = worker, make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
	return harness
}

// run starts the Flow with a unique ID and waits for its terminal status.
func (harness *textCopyHarness) run(t *testing.T, flowIDPrefix string, input Input) textCopyRun {
	t.Helper()
	flowID := harness.start(t, flowIDPrefix, input)
	return harness.wait(t, flowID)
}

func (harness *textCopyHarness) start(t *testing.T, flowIDPrefix string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	flowID := flowIDPrefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func (harness *textCopyHarness) wait(t *testing.T, flowID string) textCopyRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	run := textCopyRun{status: result.Status, errorMessage: result.ErrorMessage}
	if result.Status == dex.FlowCompleted {
		require.NoError(t, result.DecodeSingleOutput(&run.outcome))
	}
	return run
}

func (harness *textCopyHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{FlowServiceAddress: harness.serverAddress})
	require.NoError(t, err)
	harness.worker, harness.workerResult = worker, make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
	registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelRegistration()
	require.NoError(t, worker.WaitPullRegistered(registrationContext))
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
