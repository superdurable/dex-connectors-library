//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	triggerApprovalWaiting  = "waiting"
	triggerApprovalApproved = "approved"
)

var triggerApprovalState = dex.DefineAttribute[triggerApprovalRecord]("trigger-approval-state")

type triggerApprovalEvent struct {
	ThreadID string `json:"threadId"`
	// Text stands in for provider message text, which must never reach a log record.
	Text string `json:"text"`
}

type triggerApprovalStartInput struct {
	EventID string `json:"eventId"`
}

type triggerApprovalInput struct {
	EventID string `json:"eventId"`
}

type triggerApprovalRecord struct {
	StartEventID    string `json:"startEventId"`
	Status          string `json:"status"`
	ApprovalEventID string `json:"approvalEventId"`
	ApprovalCount   int    `json:"approvalCount"`
}

type triggerApprovalResult struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate"`
	Status    string `json:"status"`
}

// triggerApprovalFlow mirrors the Slack thread-approval example: one start, one approval, then completion.
type triggerApprovalFlow struct {
	dex.FlowDefaults
}

func (*triggerApprovalFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(triggerApprovalStartStep{}),
		dex.DefineStep(triggerApprovalCompleteStep{}),
	}
}

func (flow *triggerApprovalFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.Approve, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(triggerApprovalState)}}),
		dex.DefineRPC(flow.GetApprovalState, nil),
	}
}

func (*triggerApprovalFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{triggerApprovalState}}
}

// Approve accepts the first approval while the Flow waits and treats a repeated event ID as a duplicate.
func (*triggerApprovalFlow) Approve(ctx dex.Context, input triggerApprovalInput) (*dex.RPCResult[triggerApprovalResult], error) {
	if input.EventID == "" {
		return nil, fmt.Errorf("approval event ID is required")
	}
	state, err := triggerApprovalState.Get(ctx)
	if err != nil {
		return nil, err
	}
	if state.ApprovalEventID == input.EventID {
		return &dex.RPCResult[triggerApprovalResult]{Output: triggerApprovalResult{Duplicate: true, Status: state.Status}}, nil
	}
	if state.Status != triggerApprovalWaiting {
		return &dex.RPCResult[triggerApprovalResult]{Output: triggerApprovalResult{Status: state.Status}}, nil
	}
	state.Status = triggerApprovalApproved
	state.ApprovalEventID = input.EventID
	state.ApprovalCount++
	if err := triggerApprovalState.Set(ctx, state); err != nil {
		return nil, err
	}
	return &dex.RPCResult[triggerApprovalResult]{
		Output:    triggerApprovalResult{Accepted: true, Status: state.Status},
		NextSteps: []dex.StepMovement{dex.MovementOf[triggerApprovalRecord](triggerApprovalCompleteStep{}, state)},
	}, nil
}

// GetApprovalState reads the state without locks, so it also works before initialization.
func (*triggerApprovalFlow) GetApprovalState(ctx dex.Context, _ dex.None) (*dex.RPCResult[triggerApprovalRecord], error) {
	state, err := triggerApprovalState.Get(ctx)
	var notFound *dex.AttributeNotFoundError
	if errors.As(err, &notFound) {
		return &dex.RPCResult[triggerApprovalRecord]{Output: triggerApprovalRecord{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[triggerApprovalRecord]{Output: state}, nil
}

type triggerApprovalStartStep struct {
	dex.StepDefaultsNoWaitFor[triggerApprovalStartInput]
}

func (triggerApprovalStartStep) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triggerApprovalState)}}
}

func (triggerApprovalStartStep) Execute(ctx dex.Context, input triggerApprovalStartInput) (*dex.StepDecision, error) {
	if err := triggerApprovalState.Set(ctx, triggerApprovalRecord{StartEventID: input.EventID, Status: triggerApprovalWaiting}); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

type triggerApprovalCompleteStep struct {
	dex.StepDefaultsNoWaitFor[triggerApprovalRecord]
}

func (triggerApprovalCompleteStep) Execute(_ dex.Context, state triggerApprovalRecord) (*dex.StepDecision, error) {
	return dex.GracefulComplete(state), nil
}

// triggerApprovalRoutes are the durable start and reply targets an application would hand to its runners.
type triggerApprovalRoutes struct {
	start sdkgo.TriggerTarget[triggerApprovalEvent]
	reply sdkgo.TriggerTarget[triggerApprovalEvent]
}

// newTriggerApprovalRoutes wires the targets with logger, as an application passes its own logger. The
// application filter rejects events whose ID starts with "ignored-".
func newTriggerApprovalRoutes(
	t *testing.T,
	store *localconfig.Store,
	client *dex.Client,
	flow *triggerApprovalFlow,
	logger *slog.Logger,
) triggerApprovalRoutes {
	t.Helper()
	accept := func(event sdkgo.TriggerEvent[triggerApprovalEvent]) bool {
		return !strings.HasPrefix(event.ID, "ignored-")
	}
	startTarget := sdkgo.NewDexFlowTriggerTarget(client, flow, accept, resolveTriggerApprovalFlowID,
		func(event sdkgo.TriggerEvent[triggerApprovalEvent]) triggerApprovalStartInput {
			return triggerApprovalStartInput{EventID: event.ID}
		}, sdkgo.WithTriggerLogger(logger.With("binding", "approval-start")))
	replyTarget := sdkgo.NewDexRPCTriggerTarget(client, flow.Approve, accept, resolveTriggerApprovalFlowID,
		func(event sdkgo.TriggerEvent[triggerApprovalEvent]) triggerApprovalInput {
			return triggerApprovalInput{EventID: event.ID}
		}, sdkgo.WithTriggerLogger(logger.With("binding", "approval-reply")))
	durableStart, err := localconfig.NewDurableTriggerTarget(store, "fixture", "local", "threadCreated", "approval-start", startTarget,
		localconfig.WithTriggerLogger(logger))
	require.NoError(t, err)
	durableReply, err := localconfig.NewDurableTriggerTarget(store, "fixture", "local", "threadReplied", "approval-reply", replyTarget,
		localconfig.WithTriggerLogger(logger))
	require.NoError(t, err)
	return triggerApprovalRoutes{start: durableStart, reply: durableReply}
}

func resolveTriggerApprovalFlowID(event sdkgo.TriggerEvent[triggerApprovalEvent]) string {
	return "trigger-delivery-" + event.Payload.ThreadID
}

func triggerApprovalEventFor(eventID string, threadID string) sdkgo.TriggerEvent[triggerApprovalEvent] {
	return sdkgo.TriggerEvent[triggerApprovalEvent]{
		ID: eventID, OccurredAt: time.Now().UTC(), Payload: triggerApprovalEvent{ThreadID: threadID, Text: sentinelText},
	}
}

// deliverAcknowledgedTrigger persists an event as a source does before provider acknowledgement, then delivers it once.
func deliverAcknowledgedTrigger(
	ctx context.Context,
	target sdkgo.TriggerTarget[triggerApprovalEvent],
	event sdkgo.TriggerEvent[triggerApprovalEvent],
) error {
	if err := sdkgo.PrepareTriggerDelivery(ctx, target, event); err != nil {
		return fmt.Errorf("prepare Trigger delivery: %w", err)
	}
	return target.HandleTrigger(ctx, event)
}

func waitForTriggerApprovalStatus(ctx context.Context, t *testing.T, client *dex.Client, flow *triggerApprovalFlow, flowID string, status string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var state triggerApprovalRecord
		return client.InvokeRPC(ctx, flowID, flow.GetApprovalState, nil, &state) == nil && state.Status == status
	}, 20*time.Second, 100*time.Millisecond)
}

func waitForTriggerApprovalOutput(ctx context.Context, t *testing.T, client *dex.Client, flowID string) triggerApprovalRecord {
	t.Helper()
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output triggerApprovalRecord
	require.NoError(t, result.DecodeSingleOutput(&output))
	return output
}

func newTriggerApprovalRunner(
	t *testing.T,
	triggerName string,
	target sdkgo.TriggerTarget[triggerApprovalEvent],
	sourceRan *atomic.Bool,
	logger *slog.Logger,
) sdkgo.TriggerRunner {
	t.Helper()
	definition := sdkgo.TriggerDefinition{
		Trigger:     sdkgo.TriggerRef{ConnectorID: "fixture", TriggerName: triggerName},
		Description: "Receive a fixture thread event.",
	}
	runner, err := sdkgo.NewTrigger(sdkgo.TriggerConfig[triggerApprovalEvent]{
		Definition: definition,
		Binding: sdkgo.TriggerBindingRef{
			Connection: sdkgo.ConnectionRef{Provider: "fixture", Name: "local"}, Trigger: definition.Trigger, Name: "approval-" + triggerName,
		},
		Source: ranTriggerSource{ran: sourceRan},
		Target: target,
		Logger: logger,
	})
	require.NoError(t, err)
	return runner
}

// ranTriggerSource records that the runner reached its source, which happens only after replay succeeds.
type ranTriggerSource struct {
	ran *atomic.Bool
}

func (source ranTriggerSource) Run(context.Context, sdkgo.TriggerTarget[triggerApprovalEvent]) error {
	source.ran.Store(true)
	return nil
}

func newTriggerInboxStore(t *testing.T) (*localconfig.Store, string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "connections.json")
	contents := fmt.Sprintf(`{"schemaVersion":%q,"connections":[]}`, localconfig.SchemaVersion)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	return store, directory
}

// pendingTriggerEventIDs lists the event IDs persisted in every Trigger inbox in directory.
func pendingTriggerEventIDs(t *testing.T, directory string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(directory, ".trigger-inbox-*.json"))
	require.NoError(t, err)
	eventIDs := []string{}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		var inbox struct {
			Events []struct {
				EventID string `json:"eventId"`
			} `json:"events"`
		}
		require.NoError(t, json.Unmarshal(contents, &inbox))
		for _, event := range inbox.Events {
			eventIDs = append(eventIDs, event.EventID)
		}
	}
	return eventIDs
}

// TestDurableTriggerInboxSurvivesApprovalAfterCompletionAndRestartWithRealDex covers Slack thread-approval
// README steps 8 and 9: an approval after completion is consumed, and a restart replays past it.
func TestDurableTriggerInboxSurvivesApprovalAfterCompletionAndRestartWithRealDex(t *testing.T) {
	flow := &triggerApprovalFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, directory := newTriggerInboxStore(t)
	logs := newLogRecorder(t)
	routes := newTriggerApprovalRoutes(t, store, harness.client, flow, logs.logger())
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	threadA := "thread-a-" + testRunID
	flowA := resolveTriggerApprovalFlowID(triggerApprovalEventFor("", threadA))

	rootA := triggerApprovalEventFor("root-a-"+testRunID, threadA)
	require.NoError(t, deliverAcknowledgedTrigger(ctx, routes.start, rootA))
	firstApproval := triggerApprovalEventFor("approve-a-"+testRunID, threadA)
	require.Eventually(t, func() bool {
		return deliverAcknowledgedTrigger(ctx, routes.reply, firstApproval) == nil
	}, 20*time.Second, 100*time.Millisecond)
	completedA := waitForTriggerApprovalOutput(ctx, t, harness.client, flowA)
	require.Equal(t, triggerApprovalRecord{
		StartEventID: rootA.ID, Status: triggerApprovalApproved, ApprovalEventID: firstApproval.ID, ApprovalCount: 1,
	}, completedA)
	require.Empty(t, pendingTriggerEventIDs(t, directory))
	started := logs.find("trigger event delivered", map[string]string{"event_id": rootA.ID})
	require.Len(t, started, 1)
	require.Equal(t, slog.LevelDebug, started[0].level)
	require.Equal(t, map[string]string{
		"binding": "approval-start", "target": "flow_start", "event_id": rootA.ID, "flow_id": flowA, "duplicate": "false",
	}, started[0].attrs)
	approved := logs.find("trigger event delivered", map[string]string{"event_id": firstApproval.ID})
	require.Len(t, approved, 1)
	require.Equal(t, map[string]string{
		"binding": "approval-reply", "target": "rpc", "event_id": firstApproval.ID, "flow_id": flowA,
	}, approved[0].attrs)

	t.Run("step 8 approval after completion is consumed", func(t *testing.T) {
		lateApproval := triggerApprovalEventFor("approve-a-late-"+testRunID, threadA)
		require.NoError(t, deliverAcknowledgedTrigger(ctx, routes.reply, lateApproval))
		require.Empty(t, pendingTriggerEventIDs(t, directory))
		require.Equal(t, completedA, waitForTriggerApprovalOutput(ctx, t, harness.client, flowA))
		skipped := logs.find("trigger event skipped: undeliverable", map[string]string{"event_id": lateApproval.ID})
		require.Len(t, skipped, 1)
		require.Equal(t, slog.LevelWarn, skipped[0].level)
		require.Equal(t, map[string]string{
			"connector": "fixture", "connection": "local", "trigger": "threadReplied", "binding": "approval-reply",
			"event_id": lateApproval.ID, "flow_id": flowA,
			"error": fmt.Sprintf(`Trigger event is undeliverable: dex: InvokeRPC flow %q: NotFound: workflow execution already completed`, flowA),
		}, skipped[0].attrs)
	})

	t.Run("an approval the application filter rejects is consumed", func(t *testing.T) {
		ignored := triggerApprovalEventFor("ignored-approve-a-"+testRunID, threadA)
		require.NoError(t, deliverAcknowledgedTrigger(ctx, routes.reply, ignored))
		require.Empty(t, pendingTriggerEventIDs(t, directory))
		filtered := logs.find("trigger event skipped: filtered", nil)
		require.Len(t, filtered, 1)
		require.Equal(t, slog.LevelInfo, filtered[0].level)
		require.Equal(t, map[string]string{"binding": "approval-reply", "target": "rpc", "event_id": ignored.ID}, filtered[0].attrs)
	})

	t.Run("step 9 restart replays past consumed events", func(t *testing.T) {
		threadB := "thread-b-" + testRunID
		flowB := resolveTriggerApprovalFlowID(triggerApprovalEventFor("", threadB))
		rootB := triggerApprovalEventFor("root-b-"+testRunID, threadB)
		approvalB := triggerApprovalEventFor("approve-b-"+testRunID, threadB)
		// The first approval stays pending as if the process crashed after Dex applied it but before
		// inbox cleanup. The other approval arrived after completion. Neither is delivered before restart.
		for _, pending := range []struct {
			target sdkgo.TriggerTarget[triggerApprovalEvent]
			event  sdkgo.TriggerEvent[triggerApprovalEvent]
		}{
			{routes.reply, firstApproval},
			{routes.reply, triggerApprovalEventFor("approve-a-after-restart-"+testRunID, threadA)},
			{routes.start, rootB},
			{routes.reply, approvalB},
		} {
			require.NoError(t, sdkgo.PrepareTriggerDelivery(ctx, pending.target, pending.event))
		}
		require.Subset(t, pendingTriggerEventIDs(t, directory), []string{
			firstApproval.ID, "approve-a-after-restart-" + testRunID, rootB.ID, approvalB.ID,
		})

		restarted := newTriggerApprovalRoutes(t, store, harness.client, flow, logs.logger())
		var startSourceRan, replySourceRan atomic.Bool
		require.NoError(t, newTriggerApprovalRunner(t, "threadCreated", restarted.start, &startSourceRan, logs.logger()).Run(ctx))
		require.NoError(t, newTriggerApprovalRunner(t, "threadReplied", restarted.reply, &replySourceRan, logs.logger()).Run(ctx))
		require.True(t, startSourceRan.Load())
		require.True(t, replySourceRan.Load())

		require.Equal(t, triggerApprovalRecord{
			StartEventID: rootB.ID, Status: triggerApprovalApproved, ApprovalEventID: approvalB.ID, ApprovalCount: 1,
		}, waitForTriggerApprovalOutput(ctx, t, harness.client, flowB))
		require.Equal(t, completedA, waitForTriggerApprovalOutput(ctx, t, harness.client, flowA))
		require.Empty(t, pendingTriggerEventIDs(t, directory))

		for _, replay := range []struct {
			binding                   string
			count, delivered, skipped string
		}{{"approval-start", "1", "1", "0"}, {"approval-reply", "3", "1", "2"}} {
			startedReplay := logs.find("replaying pending trigger events", map[string]string{"binding": replay.binding})
			require.Len(t, startedReplay, 1, replay.binding)
			require.Equal(t, slog.LevelInfo, startedReplay[0].level)
			require.Equal(t, replay.count, startedReplay[0].attrs["count"])
			finished := logs.find("finished replaying pending trigger events", map[string]string{"binding": replay.binding})
			require.Len(t, finished, 1, replay.binding)
			require.Equal(t, slog.LevelInfo, finished[0].level)
			require.Equal(t, map[string]string{
				"connector": "fixture", "connection": "local", "trigger": map[string]string{
					"approval-start": "threadCreated", "approval-reply": "threadReplied",
				}[replay.binding], "binding": replay.binding,
				"delivered": replay.delivered, "skipped": replay.skipped, "remaining": "0",
			}, finished[0].attrs)
		}
		for _, eventID := range []string{firstApproval.ID, "approve-a-after-restart-" + testRunID} {
			require.Len(t, logs.find("trigger event skipped: undeliverable", map[string]string{"event_id": eventID}), 1, eventID)
		}
		require.NotContains(t, logs.text(), sentinelText)
	})
}

// TestDurableRPCTriggerConsumesReplyWithoutFlowWithRealDex covers an approval in a thread that never started a Flow.
func TestDurableRPCTriggerConsumesReplyWithoutFlowWithRealDex(t *testing.T) {
	flow := &triggerApprovalFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, directory := newTriggerInboxStore(t)
	logs := newLogRecorder(t)
	routes := newTriggerApprovalRoutes(t, store, harness.client, flow, logs.logger())
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)

	orphanApproval := triggerApprovalEventFor("approve-orphan-"+testRunID, "thread-orphan-"+testRunID)
	require.NoError(t, deliverAcknowledgedTrigger(ctx, routes.reply, orphanApproval))
	require.Empty(t, pendingTriggerEventIDs(t, directory))
	orphanSkip := logs.find("trigger event skipped: undeliverable", map[string]string{"event_id": orphanApproval.ID})
	require.Len(t, orphanSkip, 1)
	require.Equal(t, slog.LevelWarn, orphanSkip[0].level)
	require.Contains(t, orphanSkip[0].attrs["error"], "workflow not found")
	require.Equal(t, resolveTriggerApprovalFlowID(orphanApproval), orphanSkip[0].attrs["flow_id"])

	restarted := newTriggerApprovalRoutes(t, store, harness.client, flow, logs.logger())
	replayer, ok := restarted.reply.(sdkgo.TriggerDeliveryReplayer)
	require.True(t, ok)
	require.NoError(t, replayer.ReplayTriggerDeliveries(ctx))

	threadE := "thread-e-" + testRunID
	rootE := triggerApprovalEventFor("root-e-"+testRunID, threadE)
	approvalE := triggerApprovalEventFor("approve-e-"+testRunID, threadE)
	require.NoError(t, deliverAcknowledgedTrigger(ctx, restarted.start, rootE))
	require.Eventually(t, func() bool {
		return deliverAcknowledgedTrigger(ctx, restarted.reply, approvalE) == nil
	}, 20*time.Second, 100*time.Millisecond)
	require.Equal(t, triggerApprovalRecord{
		StartEventID: rootE.ID, Status: triggerApprovalApproved, ApprovalEventID: approvalE.ID, ApprovalCount: 1,
	}, waitForTriggerApprovalOutput(ctx, t, harness.client, resolveTriggerApprovalFlowID(rootE)))
	require.Empty(t, pendingTriggerEventIDs(t, directory))
	require.NotContains(t, logs.text(), sentinelText)
}

// TestDurableTriggerReplayWaitsForUnavailableWorkerWithRealDex covers a Worker outage while an approval is pending at startup.
func TestDurableTriggerReplayWaitsForUnavailableWorkerWithRealDex(t *testing.T) {
	flow := &triggerApprovalFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	store, directory := newTriggerInboxStore(t)
	logs := newLogRecorder(t)
	routes := newTriggerApprovalRoutes(t, store, harness.client, flow, logs.logger())
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	threadD := "thread-d-" + testRunID
	rootD := triggerApprovalEventFor("root-d-"+testRunID, threadD)
	flowD := resolveTriggerApprovalFlowID(rootD)
	require.NoError(t, deliverAcknowledgedTrigger(ctx, routes.start, rootD))
	waitForTriggerApprovalStatus(ctx, t, harness.client, flow, flowD, triggerApprovalWaiting)

	approvalD := triggerApprovalEventFor("approve-d-"+testRunID, threadD)
	require.NoError(t, sdkgo.PrepareTriggerDelivery(ctx, routes.reply, approvalD))
	harness.stopWorker(t)

	restarted := newTriggerApprovalRoutes(t, store, harness.client, flow, logs.logger())
	replayer, ok := restarted.reply.(sdkgo.TriggerDeliveryReplayer)
	require.True(t, ok)
	replayResult := make(chan error, 1)
	replayDone := make(chan struct{})
	go func() {
		replayResult <- replayer.ReplayTriggerDeliveries(ctx)
		close(replayDone)
	}()
	require.Never(t, func() bool {
		select {
		case <-replayDone:
			return true
		default:
			return false
		}
	}, 3*time.Second, 100*time.Millisecond, "replay must wait for the Worker instead of returning")
	require.Equal(t, []string{approvalD.ID}, pendingTriggerEventIDs(t, directory))

	harness.startWorker(t)
	select {
	case <-replayDone:
	case <-time.After(time.Minute):
		t.Fatal("replay did not deliver the pending approval after the Worker restarted")
	}
	require.NoError(t, <-replayResult)
	require.Equal(t, triggerApprovalRecord{
		StartEventID: rootD.ID, Status: triggerApprovalApproved, ApprovalEventID: approvalD.ID, ApprovalCount: 1,
	}, waitForTriggerApprovalOutput(ctx, t, harness.client, flowD))
	require.Empty(t, pendingTriggerEventIDs(t, directory))

	// The replay logs every backoff with its attempt and the scheduled delay, then the recovery.
	replayIdentity := map[string]string{"binding": "approval-reply", "event_id": approvalD.ID}
	retries := logs.find("trigger delivery failed; retrying", replayIdentity)
	require.GreaterOrEqual(t, len(retries), 2, "the 3-second Worker outage spans at least two backoffs")
	schedule := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 16 * time.Second, 30 * time.Second}
	for index, record := range retries {
		require.Equal(t, slog.LevelWarn, record.level)
		require.Equal(t, strconv.Itoa(index+1), record.attrs["attempt"])
		require.Equal(t, schedule[min(index, len(schedule)-1)].String(), record.attrs["delay"])
		require.Equal(t, "threadReplied", record.attrs["trigger"])
		require.NotEmpty(t, record.attrs["error"])
	}
	recovered := logs.find("trigger delivered after retry", replayIdentity)
	require.Len(t, recovered, 1)
	require.Equal(t, slog.LevelInfo, recovered[0].level)
	require.Equal(t, strconv.Itoa(len(retries)+1), recovered[0].attrs["attempts"])
	require.Len(t, logs.find("replaying pending trigger events", map[string]string{"binding": "approval-reply", "count": "1"}), 1)
	require.Len(t, logs.find("finished replaying pending trigger events", map[string]string{
		"binding": "approval-reply", "delivered": "1", "skipped": "0", "remaining": "0",
	}), 1)
	require.NotContains(t, logs.text(), sentinelText)
}
