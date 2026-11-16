//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordchangedintake

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/go"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/go/internal/airtablefake"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/go/internal/projectstoretest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestRecordChangedIntakeStartsOneFlowPerChangeAcrossRestartsWithRealDex wires the durable recordChanged
// runner as NewProjectRecordChangedTriggerRunner does for main.go: existing records start nothing, each
// created or edited record starts exactly one Flow that reads it back and completes, a renamed field keeps
// working, and changes made while the runner is stopped arrive after a restart that first meets an Airtable outage.
func TestRecordChangedIntakeStartsOneFlowPerChangeAcrossRestartsWithRealDex(t *testing.T) {
	// Unique record times per run keep event IDs, and so Flow IDs, unique on a shared Dex Server.
	base := time.Now().UTC().Truncate(time.Millisecond)
	record := func(id string, createdAt time.Time, modifiedAt time.Time) airtablefake.Record {
		return airtablefake.Record{ID: id, CreatedAt: createdAt, ModifiedAt: modifiedAt, Fields: map[string]any{"Email": id + "@example.test", "Status": "New"}}
	}
	table := airtablefake.New(t, record("recExisting000001", base, base))
	harness := newIntakeHarness(t, table)
	store := projectstoretest.New()
	logs := &lockedBuffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	firstRun := harness.startRunner(t, ctx, table, store, logs)
	waitForRequests(t, table, 3) // Schema, seed, and one poll after it.
	table.Put(record("recLeadAda0000001", base.Add(time.Second), base.Add(time.Second)), record("recLeadGrace00001", base.Add(time.Second), base.Add(time.Second)))
	ada := harness.requireCompletedChange(t, ctx, "recLeadAda0000001")
	require.True(t, ada.Change.IsCreatedRecord)
	require.Equal(t, airtablefake.BaseID, ada.Change.BaseID)
	require.Equal(t, base.Add(time.Second), ada.Change.LastModifiedTime)
	email, isText := ada.Change.Fields["Email"].Text()
	require.True(t, isText, "typed cell values survive Dex persistence")
	require.Equal(t, "recLeadAda0000001@example.test", email)
	currentEmail, isText := ada.CurrentFields["Email"].Text()
	require.True(t, isText, "the Flow read the record back through the getRecord Step")
	require.Equal(t, "recLeadAda0000001@example.test", currentEmail)
	harness.requireCompletedChange(t, ctx, "recLeadGrace00001")

	// An edit of an existing record is a change; renaming the watched field does not stop the Trigger.
	table.RenameField("Updated At")
	edited := record("recExisting000001", base, base.Add(2*time.Second))
	edited.Fields["Status"] = "Qualified"
	table.Put(edited)
	existing := harness.requireCompletedChange(t, ctx, "recExisting000001")
	require.False(t, existing.Change.IsCreatedRecord)
	status, isText := existing.CurrentFields["Status"].Text()
	require.True(t, isText)
	require.Equal(t, "Qualified", status, "the read-back record carries the edit")
	firstRun.stop(t)

	table.Put(record("recWhileStopped01", base.Add(3*time.Second), base.Add(3*time.Second)))
	table.Script(airtablefake.ScriptedResponse{Status: http.StatusServiceUnavailable, ErrorType: "SERVICE_UNAVAILABLE"})
	secondRun := harness.startRunner(t, ctx, table, store, logs)
	harness.requireCompletedChange(t, ctx, "recWhileStopped01")
	requestsBefore := table.CompletedRequests()
	waitForRequests(t, table, requestsBefore+2)
	secondRun.stop(t)

	require.Len(t, harness.deliveredEventIDs(), 4, "existing records start no Flow: %v", harness.deliveredEventIDs())
	for _, eventID := range harness.deliveredEventIDs() {
		require.Equal(t, 1, harness.deliveryCount(eventID), "event %s is recorded once and never replayed", eventID)
	}
	pending, err := store.PendingEventIDs(ctx)
	require.NoError(t, err)
	require.Empty(t, pending)
	text := logs.String()
	require.Contains(t, text, "poll cursor seeded")
	require.NotContains(t, text, "SENTINEL")
	require.NotContains(t, text, airtablefake.AccessToken)
	require.NotContains(t, text, "@example.test", "records never contain field values")
}

// intakeHarness runs the example Flow on a real Dex Server and counts Flow-start deliveries per event ID.
type intakeHarness struct {
	flow       *Flow
	client     *dex.Client
	mutex      sync.Mutex
	deliveries map[string]int
	order      []string
}

func newIntakeHarness(t *testing.T, table *airtablefake.Table) *intakeHarness {
	t.Helper()
	// The Flow's getRecord Step reads from the fake through its own client, as main.go's project connection does.
	flowClient, err := airtable.New(airtable.Config{Endpoint: table.URL}, sdkgo.StaticCredentialProvider[airtable.Credentials]{
		{Provider: "airtable", Name: ConnectionName}: {PersonalAccessToken: sdkgo.NewSecretString(airtablefake.AccessToken)},
	})
	require.NoError(t, err)
	connection, err := airtable.NewConnection(flowClient, sdkgo.ConnectionRef{Provider: "airtable", Name: ConnectionName})
	require.NoError(t, err)
	flow := NewFlow(connection)
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
	t *testing.T, parent context.Context, table *airtablefake.Table, store *projectstoretest.Store, logs *lockedBuffer,
) *runningIntakeRunner {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reference := sdkgo.ConnectionRef{Provider: "airtable", Name: ConnectionName}
	client, err := airtable.New(airtable.Config{Endpoint: table.URL, PollInterval: time.Second}, sdkgo.StaticCredentialProvider[airtable.Credentials]{
		reference: {PersonalAccessToken: sdkgo.NewSecretString(airtablefake.AccessToken)},
	}, airtable.WithLogger(logger))
	require.NoError(t, err)
	connection, err := airtable.NewConnection(client, reference)
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{
		ConnectorID: airtable.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: airtable.RecordChangedTriggerDefinition.Trigger.TriggerName, BindingName: RecordChangedTriggerBinding,
	}
	inbox, err := store.Inbox(key)
	require.NoError(t, err)
	flowTarget := sdkgo.NewDexFlowTriggerTarget(harness.client, harness.flow, AcceptChangedRecord, ResolveFlowID, MapToFlowInput, sdkgo.WithTriggerLogger(logger))
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTargetFunc[airtable.RecordChangedEvent](
		func(ctx context.Context, event sdkgo.TriggerEvent[airtable.RecordChangedEvent]) error {
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
	runner, err := airtable.NewRecordChangedTriggerRunner(airtable.RecordChangedTriggerRunnerConfig{
		Connection: connection,
		Routes: []airtable.RecordChangedTriggerRoute{{
			BindingName: RecordChangedTriggerBinding, Cursor: cursor, Target: target,
			Configuration: airtable.RecordChangedTriggerConfiguration{
				BaseID: airtablefake.BaseID, TableID: airtablefake.TableID, LastModifiedTimeFieldID: airtablefake.FieldID,
			},
		}},
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
		t.Fatal("recordChanged runner did not stop after cancellation")
	}
}

// requireCompletedChange waits for the next unclaimed delivered event of recordID, then for its Flow to read the
// record back and complete.
func (harness *intakeHarness) requireCompletedChange(t *testing.T, ctx context.Context, recordID string) Intake {
	t.Helper()
	var eventID string
	require.Eventually(t, func() bool {
		for _, delivered := range harness.deliveredEventIDs() {
			if strings.HasPrefix(delivered, recordID+"-") && !harness.isClaimed(delivered) {
				eventID = delivered
				return true
			}
		}
		return false
	}, 30*time.Second, 50*time.Millisecond, "a change of %s is delivered", recordID)
	harness.claim(eventID)
	result, err := harness.client.WaitForFlow(ctx, FlowIDPrefix+eventID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var intake Intake
	require.NoError(t, result.DecodeSingleOutput(&intake))
	require.Equal(t, eventID, intake.Change.EventID)
	require.Equal(t, recordID, intake.Change.RecordID)
	require.True(t, intake.IsCompleted)
	require.False(t, intake.IsRemoved)
	return intake
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

// claimed event IDs were already returned by requireCompletedChange, so a record changed again finds its new event.
var claimedEventIDs sync.Map

func (harness *intakeHarness) isClaimed(eventID string) bool {
	_, isClaimed := claimedEventIDs.Load(eventID)
	return isClaimed
}

func (harness *intakeHarness) claim(eventID string) { claimedEventIDs.Store(eventID, true) }

// waitForRequests waits until the fake provider has answered count requests.
func waitForRequests(t *testing.T, table *airtablefake.Table, count int) {
	t.Helper()
	require.Eventually(t, func() bool { return table.CompletedRequests() >= count }, 30*time.Second, 20*time.Millisecond)
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
