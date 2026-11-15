//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fileintake

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive/go/internal/drivechangetest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAccessToken = "drive-trigger-integration-token"
	watchedFolderID        = "fld_inbox"
)

// TestFileIntakeStartsOneFlowPerCreatedFileAcrossRestartsWithRealDex wires the example as main.go
// does, against a fake Drive change log and a real Dex server.
func TestFileIntakeStartsOneFlowPerCreatedFileAcrossRestartsWithRealDex(t *testing.T) {
	changeLog := drivechangetest.New(integrationAccessToken)
	t.Cleanup(changeLog.Close)
	runID := strconv.FormatInt(time.Now().UnixNano(), 36)
	fileID := func(name string) string { return name + "_" + runID }
	hourAgo := drivechangetest.Now().Add(-time.Hour)
	changeLog.Put(drivechangetest.File{ID: fileID("backlog"), Name: "Backlog.pdf", MimeType: "application/pdf", Parent: watchedFolderID, CreatedTime: hourAgo, ModifiedTime: hourAgo})
	project := loadIntakeProject(t, changeLog.URL)
	harness := newIntakeHarness(t, project)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	firstRun := harness.startRunner(t, ctx, project)
	require.Eventually(t, func() bool { return changeLog.ListPages() >= 1 }, 30*time.Second, 50*time.Millisecond)
	changeLog.CreateFile(fileID("invoice"), "Invoice.pdf", "application/pdf", watchedFolderID)
	changeLog.CreateFile(fileID("notes"), "Notes", "application/vnd.google-apps.document", watchedFolderID)
	changeLog.CreateFile(fileID("elsewhere"), "Elsewhere.pdf", "application/pdf", "fld_other")
	changeLog.ModifyFile(fileID("backlog"), drivechangetest.Now())
	invoice := waitForIntake(t, ctx, harness.client, fileID("invoice"))
	require.Equal(t, fileID("invoice")+"@created", invoice.EventID)
	require.Equal(t, "Invoice.pdf", invoice.Reported.Name)
	require.Equal(t, []string{watchedFolderID}, invoice.Reported.Parents)
	require.NotNil(t, invoice.Current)
	require.Equal(t, fileID("invoice"), invoice.Current.ID)
	firstRun.stop(t)

	// A file created while the runner is stopped starts its Flow after the restart.
	changeLog.CreateFile(fileID("scan"), "Scan.pdf", "application/pdf", watchedFolderID)
	secondRun := harness.startRunner(t, ctx, project)
	scan := waitForIntake(t, ctx, harness.client, fileID("scan"))
	require.Equal(t, "Scan.pdf", scan.Reported.Name)
	pages := changeLog.ListPages()
	require.Eventually(t, func() bool { return changeLog.ListPages() >= pages+2 }, 30*time.Second, 50*time.Millisecond)
	secondRun.stop(t)

	for _, filtered := range []string{"backlog", "notes", "elsewhere"} {
		_, err := harness.client.DescribeFlow(ctx, ResolveFlowID(sdkgo.TriggerEvent[drive.FileEvent]{Payload: drive.FileEvent{FileID: fileID(filtered)}}))
		var notFound *dex.FlowNotFoundError
		require.ErrorAs(t, err, &notFound, filtered)
	}
	require.Equal(t, map[string]int{fileID("invoice") + "@created": 1, fileID("scan") + "@created": 1}, harness.deliveries())
	require.Equal(t, 1, changeLog.Requests("/drive/v3/changes/startPageToken"), "the restart resumed the stored cursor")
}

// loadIntakeProject writes the files Dex Web saves under dexcli dev and loads them as main.go does.
// The loaded project outlives each runner, as a replica's project storage does.
func loadIntakeProject(t *testing.T, endpoint string) *projectconfig.LoadedProject {
	t.Helper()
	directory := t.TempDir()
	expiresAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	writeIntakeJSON(t, filepath.Join(directory, "connections.json"), map[string]any{
		"schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
		"connections": []any{map[string]any{
			"connectorId": drive.ConnectorID, "authMethodId": drive.GoogleOAuthAuthMethodID, "provider": "google",
			"modulePath": "github.com/superdurable/dex-connectors-library/connectors/google/drive/go", "connectionName": ConnectionName,
			"configuration": map[string]any{"endpoint": endpoint, "pollInterval": int64(time.Second)},
			"credentials": map[string]any{
				"oauth_client_id": "client.apps.googleusercontent.com", "oauth_client_secret": "client-secret",
				"access_token": integrationAccessToken, "refresh_token": "refresh-token",
			},
			"credentialExpiresAt": expiresAt,
		}},
		"triggerBindings": []any{map[string]any{
			"connectorId": drive.ConnectorID, "connectionName": ConnectionName, "triggerName": drive.FileCreatedTriggerDefinition.Trigger.TriggerName,
			"bindingName":   NewFileTriggerBinding,
			"configuration": map[string]any{"folderId": watchedFolderID, "folderName": "Inbox", "mimeType": "application/pdf"},
		}},
	})
	writeIntakeJSON(t, filepath.Join(directory, "use-configurations.json"), map[string]any{
		"schemaVersion": "connectors.dex.dev/local-use-configurations/v1alpha1", "operationConfigurations": []any{},
	})
	project, err := projectconfig.LoadDexWebConnectionsFile(context.Background(), filepath.Join(directory, "connections.json"))
	require.NoError(t, err)
	return project
}

func writeIntakeJSON(t *testing.T, path string, value any) {
	t.Helper()
	contents, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	require.NoError(t, os.Chmod(path, 0o600))
}

// intakeHarness owns the Worker and Client, and counts the target's deliveries per event.
type intakeHarness struct {
	flow   *Flow
	client *dex.Client
	mutex  sync.Mutex
	counts map[string]int
}

func newIntakeHarness(t *testing.T, project *projectconfig.LoadedProject) *intakeHarness {
	t.Helper()
	connection, err := drive.NewProjectConnection(project, ConnectionName)
	require.NoError(t, err)
	harness := &intakeHarness{flow: NewFlow(connection), counts: map[string]int{}}
	registry, err := dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8801"
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelRegistration()
	require.NoError(t, worker.WaitPullRegistered(registrationContext))
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult, harness.client.Close(), cache.Close()))
	})
	return harness
}

type runningIntakeRunner struct {
	cancel context.CancelFunc
	result chan error
}

// startRunner builds the durable runner exactly as main.go does.
func (harness *intakeHarness) startRunner(t *testing.T, parent context.Context, project *projectconfig.LoadedProject) *runningIntakeRunner {
	t.Helper()
	flowTarget := sdkgo.NewDexFlowTriggerTarget(harness.client, harness.flow, AcceptCreatedFile, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[drive.FileEvent](func(ctx context.Context, event sdkgo.TriggerEvent[drive.FileEvent]) error {
		err := flowTarget.HandleTrigger(ctx, event)
		if err == nil {
			harness.mutex.Lock()
			harness.counts[event.ID]++
			harness.mutex.Unlock()
		}
		return err
	})
	runner, err := drive.NewProjectFileTriggerRunner(project, ConnectionName, drive.ProjectFileTriggerRunnerConfig{
		FileCreatedRoutes: []drive.ProjectFileTriggerRoute{{BindingName: NewFileTriggerBinding, Target: counting}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(parent)
	running := &runningIntakeRunner{cancel: cancel, result: make(chan error, 1)}
	go func() { running.result <- runner.Run(ctx) }()
	t.Cleanup(cancel)
	return running
}

func (running *runningIntakeRunner) stop(t *testing.T) {
	t.Helper()
	running.cancel()
	select {
	case err := <-running.result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("Google Drive file Trigger runner did not stop after cancellation")
	}
}

func (harness *intakeHarness) deliveries() map[string]int {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	counts := make(map[string]int, len(harness.counts))
	for eventID, count := range harness.counts {
		counts[eventID] = count
	}
	return counts
}

// waitForIntake polls until the Trigger started the file's Flow, then waits for its outcome.
func waitForIntake(t *testing.T, ctx context.Context, client *dex.Client, fileID string) Outcome {
	t.Helper()
	flowID := ResolveFlowID(sdkgo.TriggerEvent[drive.FileEvent]{Payload: drive.FileEvent{FileID: fileID}})
	require.Eventually(t, func() bool {
		_, err := client.DescribeFlow(ctx, flowID)
		return err == nil
	}, 60*time.Second, 100*time.Millisecond, "the Trigger never started %s", flowID)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}
