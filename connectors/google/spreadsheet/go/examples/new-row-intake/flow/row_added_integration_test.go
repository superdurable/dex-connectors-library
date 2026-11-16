//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newrowintake

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go/internal/projectstoretest"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go/internal/sheetsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestNewRowIntakeStartsOneFlowPerAddedRowAcrossRestartsWithRealDex wires the durable rowAdded runner as
// NewProjectRowAddedTriggerRunner does for main.go: existing rows start nothing, each appended row starts
// exactly one completed Flow that reads the row back, and rows added while the runner is stopped arrive
// after a restart.
func TestNewRowIntakeStartsOneFlowPerAddedRowAcrossRestartsWithRealDex(t *testing.T) {
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	header := []string{"email", "name", "run"}
	sheet := sheetsfake.New(t, "Leads", header, []string{"existing@example.test", "Existing", testRunID})
	harness := newIntakeHarness(t, sheet)
	store := projectstoretest.New()
	logs := &lockedBuffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	firstRun := harness.startRunner(t, ctx, sheet, store, logs)
	waitForRequests(t, sheet, 2) // The seed and one poll after it.
	sheet.AppendRows([]string{"ada@example.test", "Ada", testRunID}, []string{}, []string{"grace@example.test", "Grace", testRunID})
	adaOutcome := harness.requireCompletedOutcome(t, ctx, 3)
	ada := adaOutcome.Row
	require.Equal(t, []string{"ada@example.test", "Ada", testRunID}, adaOutcome.CurrentCells, "the Flow reads the row back")
	require.Contains(t, sheet.ReadRanges(), "'Leads'!3:3")
	require.Equal(t, AddedRow{
		EventID: ada.EventID, SpreadsheetID: "sheet-1", SheetName: "Leads", RowNumber: 3,
		Values: map[string]string{"email": "ada@example.test", "name": "Ada", "run": testRunID}, DetectedAt: ada.DetectedAt,
	}, ada)
	harness.requireCompletedRow(t, ctx, 5)

	// Deleting a row above the cursor shifts every row up; the runner realigns and delivers only the new row.
	sheet.DeleteRow(2)
	sheet.AppendRows([]string{"linus@example.test", "Linus", testRunID})
	linus := harness.requireCompletedRow(t, ctx, 5)
	require.Equal(t, "linus@example.test", linus.Values["email"])
	firstRun.stop(t)

	sheet.AppendRows([]string{"while-stopped@example.test", "Stopped", testRunID})
	sheet.Script(sheetsfake.ScriptedResponse{Status: http.StatusTooManyRequests, RetryAfter: "1"})
	secondRun := harness.startRunner(t, ctx, sheet, store, logs)
	stopped := harness.requireCompletedRow(t, ctx, 6)
	require.Equal(t, "while-stopped@example.test", stopped.Values["email"])
	requestsBefore := sheet.CompletedRequests()
	waitForRequests(t, sheet, requestsBefore+2)
	secondRun.stop(t)

	require.Equal(t, 4, len(harness.deliveredEventIDs()), "existing rows start no Flow: %v", harness.deliveredEventIDs())
	for _, eventID := range harness.deliveredEventIDs() {
		require.Equal(t, 1, harness.deliveryCount(eventID), "event %s is recorded once and never replayed", eventID)
	}
	pending, err := store.PendingEventIDs(ctx)
	require.NoError(t, err)
	require.Empty(t, pending)
	text := logs.String()
	require.Contains(t, text, "poll cursor seeded")
	require.Contains(t, text, "sheet rows moved; cursor realigned")
	require.NotContains(t, text, "SENTINEL")
	require.NotContains(t, text, "@example.test", "records never contain cell values")
}

// intakeHarness runs the example Flow on a real Dex Server and counts Flow-start deliveries per event ID.
type intakeHarness struct {
	flow       *Flow
	client     *dex.Client
	mutex      sync.Mutex
	deliveries map[string]int
	order      []string
}

func newIntakeHarness(t *testing.T, sheet *sheetsfake.Sheet) *intakeHarness {
	t.Helper()
	flow := NewFlow(newSheetsConnection(t, sheet, slog.New(slog.DiscardHandler)))
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelRegistration()
	require.NoError(t, worker.WaitPullRegistered(registrationContext))
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult, client.Close(), cache.Close()))
	})
	return &intakeHarness{flow: flow, client: client, deliveries: map[string]int{}}
}

type runningIntakeRunner struct {
	cancel context.CancelFunc
	result chan error
}

// startRunner builds the runner over the same project store each time, as a restarted replica finds it.
func (harness *intakeHarness) startRunner(
	t *testing.T, parent context.Context, sheet *sheetsfake.Sheet, store *projectstoretest.Store, logs *lockedBuffer,
) *runningIntakeRunner {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	connection := newSheetsConnection(t, sheet, logger)
	key := projectconfig.TriggerInboxKey{
		ConnectorID: spreadsheet.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: spreadsheet.RowAddedTriggerDefinition.Trigger.TriggerName, BindingName: NewRowTriggerBinding,
	}
	inbox, err := store.Inbox(key)
	require.NoError(t, err)
	flowTarget := sdkgo.NewDexFlowTriggerTarget(harness.client, harness.flow, AcceptAddedRow, ResolveFlowID, MapToFlowInput, sdkgo.WithTriggerLogger(logger))
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTargetFunc[spreadsheet.RowAddedEvent](
		func(ctx context.Context, event sdkgo.TriggerEvent[spreadsheet.RowAddedEvent]) error {
			if err := flowTarget.HandleTrigger(ctx, event); err != nil {
				return err
			}
			// Counted only after Dex accepted the start, so the Flow exists once the test sees the event.
			harness.recordDelivery(event.ID)
			return nil
		}), provider.WithTriggerLogger(logger))
	require.NoError(t, err)
	cursor, err := store.Cursor(projectconfig.PollCursorKey(key))
	require.NoError(t, err)
	runner, err := spreadsheet.NewRowAddedTriggerRunner(spreadsheet.RowAddedTriggerRunnerConfig{
		Connection: connection,
		Routes: []spreadsheet.RowAddedTriggerRoute{{
			BindingName: NewRowTriggerBinding, Configuration: spreadsheet.RowAddedTriggerConfiguration{SpreadsheetID: "sheet-1", SheetName: "Leads"},
			Cursor: cursor, Target: target,
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(parent)
	running := &runningIntakeRunner{cancel: cancel, result: make(chan error, 1)}
	go func() { running.result <- runner.Run(ctx) }()
	t.Cleanup(cancel)
	return running
}

// newSheetsConnection connects to the fake tab with a sentinel token that must never reach a log.
func newSheetsConnection(t *testing.T, sheet *sheetsfake.Sheet, logger *slog.Logger) spreadsheet.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: sheet.URL, PollInterval: time.Second}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("SENTINEL-ACCESS-TOKEN")},
	}, spreadsheet.WithLogger(logger))
	require.NoError(t, err)
	connection, err := spreadsheet.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func (running *runningIntakeRunner) stop(t *testing.T) {
	t.Helper()
	running.cancel()
	select {
	case err := <-running.result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("rowAdded runner did not stop after cancellation")
	}
}

// requireCompletedRow waits for the delivered event of rowNumber, then for its Flow to complete.
func (harness *intakeHarness) requireCompletedRow(t *testing.T, ctx context.Context, rowNumber int64) AddedRow {
	t.Helper()
	return harness.requireCompletedOutcome(t, ctx, rowNumber).Row
}

// requireCompletedOutcome waits for the delivered event of rowNumber, then returns its completed Flow's output.
func (harness *intakeHarness) requireCompletedOutcome(t *testing.T, ctx context.Context, rowNumber int64) Outcome {
	t.Helper()
	prefix := "row-" + strconv.FormatInt(rowNumber, 10) + "-"
	var eventID string
	require.Eventually(t, func() bool {
		for _, delivered := range harness.deliveredEventIDs() {
			if len(delivered) > len(prefix) && delivered[:len(prefix)] == prefix && harness.deliveryCount(delivered) > 0 && !harness.isClaimed(delivered) {
				eventID = delivered
				return true
			}
		}
		return false
	}, 30*time.Second, 50*time.Millisecond, "row %d is delivered", rowNumber)
	harness.claim(eventID)
	result, err := harness.client.WaitForFlow(ctx, FlowIDPrefix+eventID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, eventID, outcome.Row.EventID)
	return outcome
}

func (harness *intakeHarness) recordDelivery(eventID string) {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	if harness.deliveries[eventID] == 0 {
		harness.order = append(harness.order, eventID)
	}
	harness.deliveries[eventID]++
}

// deliveredEventIDs lists each delivered event ID once, in first-delivery order.
func (harness *intakeHarness) deliveredEventIDs() []string {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	return slices.Clone(harness.order)
}

func (harness *intakeHarness) deliveryCount(eventID string) int {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	return harness.deliveries[eventID]
}

// claimed event IDs were already returned by requireCompletedRow, so a reused row number finds its new event.
var claimedEventIDs sync.Map

func (harness *intakeHarness) isClaimed(eventID string) bool {
	_, isClaimed := claimedEventIDs.Load(eventID)
	return isClaimed
}

func (harness *intakeHarness) claim(eventID string) { claimedEventIDs.Store(eventID, true) }

// waitForRequests waits until the fake provider has answered count requests.
func waitForRequests(t *testing.T, sheet *sheetsfake.Sheet, count int) {
	t.Helper()
	require.Eventually(t, func() bool { return sheet.CompletedRequests() >= count }, 30*time.Second, 20*time.Millisecond)
}

// lockedBuffer collects log text from the runner, inbox, and Dex target goroutines.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (buffer *lockedBuffer) Write(contents []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.Write(contents)
}

func (buffer *lockedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.String()
}
