//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var triggerLedgerState = dex.DefineAttribute[triggerLedgerRecord]("trigger-ledger-state")

type triggerLedgerRecord struct {
	Total   int `json:"total"`
	Entries int `json:"entries"`
}

type triggerLedgerEntry struct {
	EventID string `json:"eventId"`
	Amount  int    `json:"amount"`
}

// triggerLedgerFlow counts handler runs to tell a repeat answered from the recorded reply from a rerun.
type triggerLedgerFlow struct {
	dex.FlowDefaults
	lockedRuns   atomic.Int32
	unlockedRuns atomic.Int32
	wiredRuns    atomic.Int32
}

func (*triggerLedgerFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(triggerLedgerStartStep{})}
}

func (flow *triggerLedgerFlow) GetRPCs() []dex.RPCDef {
	ledgerLock := []dex.AttributeLock{dex.LockAttribute(triggerLedgerState)}
	return []dex.RPCDef{
		dex.DefineRPC(flow.RecordLocked, &dex.RPCOptions{LockAttributes: ledgerLock}),
		dex.DefineRPC(flow.RecordUnlocked, nil),
		dex.DefineRPC(flow.RecordWired, &dex.RPCOptions{Name: "recordLedgerEntry", LockAttributes: ledgerLock}),
		dex.DefineRPC(flow.GetLedger, nil),
	}
}

func (*triggerLedgerFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{triggerLedgerState}}
}

// RecordLocked adds an entry under the ledger lock, so Dex answers a repeat without running it.
func (flow *triggerLedgerFlow) RecordLocked(ctx dex.Context, entry triggerLedgerEntry) (*dex.RPCResult[triggerLedgerRecord], error) {
	flow.lockedRuns.Add(1)
	return addTriggerLedgerEntry(ctx, entry)
}

// RecordUnlocked adds an entry without locks, so a repeat runs it again and Dex discards its changes.
func (flow *triggerLedgerFlow) RecordUnlocked(ctx dex.Context, entry triggerLedgerEntry) (*dex.RPCResult[triggerLedgerRecord], error) {
	flow.unlockedRuns.Add(1)
	return addTriggerLedgerEntry(ctx, entry)
}

// RecordWired is registered under the wire name recordLedgerEntry.
func (flow *triggerLedgerFlow) RecordWired(ctx dex.Context, entry triggerLedgerEntry) (*dex.RPCResult[triggerLedgerRecord], error) {
	flow.wiredRuns.Add(1)
	return addTriggerLedgerEntry(ctx, entry)
}

// GetLedger reads the ledger without locks.
func (*triggerLedgerFlow) GetLedger(ctx dex.Context, _ dex.None) (*dex.RPCResult[triggerLedgerRecord], error) {
	state, err := triggerLedgerState.Get(ctx)
	var notFound *dex.AttributeNotFoundError
	if errors.As(err, &notFound) {
		return &dex.RPCResult[triggerLedgerRecord]{Output: triggerLedgerRecord{Entries: -1}}, nil
	}
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[triggerLedgerRecord]{Output: state}, nil
}

func addTriggerLedgerEntry(ctx dex.Context, entry triggerLedgerEntry) (*dex.RPCResult[triggerLedgerRecord], error) {
	state, err := triggerLedgerState.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Total += entry.Amount
	state.Entries++
	if err := triggerLedgerState.Set(ctx, state); err != nil {
		return nil, err
	}
	return &dex.RPCResult[triggerLedgerRecord]{Output: state}, nil
}

type triggerLedgerStartStep struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (triggerLedgerStartStep) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triggerLedgerState)}}
}

func (triggerLedgerStartStep) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if err := triggerLedgerState.Set(ctx, triggerLedgerRecord{}); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// startTriggerLedger starts a ledger Flow and waits until its start Step initialized the ledger.
func startTriggerLedger(ctx context.Context, t *testing.T, harness *dexHarness, flow *triggerLedgerFlow, flowID string) {
	t.Helper()
	_, err := harness.client.StartFlow(ctx, flow, flowID, nil, dex.StartFlowOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return readTriggerLedger(ctx, harness, flow, flowID).Entries == 0
	}, 20*time.Second, 100*time.Millisecond)
}

func readTriggerLedger(ctx context.Context, harness *dexHarness, flow *triggerLedgerFlow, flowID string) triggerLedgerRecord {
	state := triggerLedgerRecord{Entries: -1}
	if err := harness.client.InvokeRPC(ctx, flowID, flow.GetLedger, nil, &state); err != nil {
		return triggerLedgerRecord{Entries: -1}
	}
	return state
}

func newTriggerLedgerTarget(
	harness *dexHarness,
	rpc dex.RPC[triggerLedgerEntry, triggerLedgerRecord],
	flowID string,
	amount int,
	options ...sdkgo.TriggerOption,
) sdkgo.TriggerTarget[triggerApprovalEvent] {
	return sdkgo.NewDexRPCTriggerTarget(harness.client, rpc,
		func(sdkgo.TriggerEvent[triggerApprovalEvent]) bool { return true },
		func(sdkgo.TriggerEvent[triggerApprovalEvent]) string { return flowID },
		func(event sdkgo.TriggerEvent[triggerApprovalEvent]) triggerLedgerEntry {
			return triggerLedgerEntry{EventID: event.ID, Amount: amount}
		}, options...)
}

func mustTriggerRPCRequestID(t *testing.T, rpcName string, eventID string) string {
	t.Helper()
	requestID, err := sdkgo.TriggerRPCRequestID(rpcName, eventID)
	require.NoError(t, err)
	return requestID
}

// TestDexRPCTriggerTargetAppliesARedeliveredEventOnceWithRealDex delivers each event twice, as an inbox
// replay or provider retry does.
func TestDexRPCTriggerTargetAppliesARedeliveredEventOnceWithRealDex(t *testing.T) {
	flow := &triggerLedgerFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := "trigger-ledger-" + testRunID
	startTriggerLedger(ctx, t, harness, flow, flowID)
	logs := newLogRecorder(t)

	t.Run("a locking RPC answers the repeat without running again", func(t *testing.T) {
		target := newTriggerLedgerTarget(harness, flow.RecordLocked, flowID, 5, sdkgo.WithTriggerLogger(logs.logger()))
		event := triggerApprovalEventFor("locked-"+testRunID, testRunID)
		require.NoError(t, target.HandleTrigger(ctx, event))
		require.NoError(t, target.HandleTrigger(ctx, event))
		require.Equal(t, triggerLedgerRecord{Total: 5, Entries: 1}, readTriggerLedger(ctx, harness, flow, flowID))
		require.Equal(t, int32(1), flow.lockedRuns.Load())
		delivered := logs.find("trigger event delivered", map[string]string{"event_id": event.ID})
		require.Len(t, delivered, 2)
		requestID := mustTriggerRPCRequestID(t, "RecordLocked", event.ID)
		for _, record := range delivered {
			require.Equal(t, map[string]string{"target": "rpc", "event_id": event.ID, "flow_id": flowID, "request_id": requestID}, record.attrs)
		}

		// The documented derivation is the ID the target sent: a Client call with it is a repeat too.
		var reply triggerLedgerRecord
		require.NoError(t, harness.client.InvokeRPCWithOptions(ctx, flowID, flow.RecordLocked, triggerLedgerEntry{EventID: event.ID, Amount: 5},
			&reply, dex.RPCInvokeOptions{RequestID: &requestID}))
		require.Equal(t, triggerLedgerRecord{Total: 5, Entries: 1}, reply)
		require.Equal(t, int32(1), flow.lockedRuns.Load())
	})

	t.Run("an RPC without locks runs again and Dex discards its changes", func(t *testing.T) {
		target := newTriggerLedgerTarget(harness, flow.RecordUnlocked, flowID, 7)
		event := triggerApprovalEventFor("unlocked-"+testRunID, testRunID)
		require.NoError(t, target.HandleTrigger(ctx, event))
		require.NoError(t, target.HandleTrigger(ctx, event))
		require.Equal(t, triggerLedgerRecord{Total: 12, Entries: 2}, readTriggerLedger(ctx, harness, flow, flowID))
		require.Equal(t, int32(2), flow.unlockedRuns.Load())
	})

	t.Run("WithTriggerRPCName derives the ID from the wire name", func(t *testing.T) {
		target := newTriggerLedgerTarget(harness, flow.RecordWired, flowID, 11, sdkgo.WithTriggerRPCName("recordLedgerEntry"))
		event := triggerApprovalEventFor("wired-"+testRunID, testRunID)
		require.NoError(t, target.HandleTrigger(ctx, event))
		requestID := mustTriggerRPCRequestID(t, "recordLedgerEntry", event.ID)
		var reply triggerLedgerRecord
		require.NoError(t, harness.client.InvokeRPCWithOptions(ctx, flowID, flow.RecordWired, triggerLedgerEntry{EventID: event.ID, Amount: 11},
			&reply, dex.RPCInvokeOptions{RequestID: &requestID}))
		require.Equal(t, triggerLedgerRecord{Total: 23, Entries: 3}, reply)
		require.Equal(t, int32(1), flow.wiredRuns.Load())
	})

	require.NotContains(t, logs.text(), sentinelText)
}

// TestDexRPCTriggerTargetAppliesAChangedRedeliveryAtMostOnceWithRealDex: Dex Server 0.1.37 and later answer
// it with the first reply; earlier servers report a conflict.
func TestDexRPCTriggerTargetAppliesAChangedRedeliveryAtMostOnceWithRealDex(t *testing.T) {
	flow := &triggerLedgerFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := "trigger-ledger-changed-" + testRunID
	startTriggerLedger(ctx, t, harness, flow, flowID)
	event := triggerApprovalEventFor("changed-"+testRunID, testRunID)
	require.NoError(t, newTriggerLedgerTarget(harness, flow.RecordLocked, flowID, 1).HandleTrigger(ctx, event))

	logs := newLogRecorder(t)
	changedTarget := newTriggerLedgerTarget(harness, flow.RecordLocked, flowID, 100, sdkgo.WithTriggerLogger(logs.logger()))
	requestID := mustTriggerRPCRequestID(t, "RecordLocked", event.ID)
	err := changedTarget.HandleTrigger(ctx, event)
	if err != nil {
		require.True(t, sdkgo.IsTriggerUndeliverable(err), "a conflict is undeliverable: %v", err)
		var conflict *sdkgo.TriggerRPCRequestConflictError
		require.ErrorAs(t, err, &conflict)
		require.Equal(t, sdkgo.TriggerRPCRequestConflictError{
			EventID: event.ID, FlowID: flowID, RPCName: "RecordLocked", RequestID: requestID, Err: conflict.Err,
		}, *conflict)
		var serviceError *dex.ServiceError
		require.ErrorAs(t, conflict.Err, &serviceError)
		dropped := logs.find("trigger rpc request conflict; event dropped", nil)
		require.Len(t, dropped, 1)
		require.Equal(t, slog.LevelError, dropped[0].level)
		require.Equal(t, map[string]string{
			"target": "rpc", "event_id": event.ID, "request_id": requestID, "rpc_name": "RecordLocked", "flow_id": flowID,
		}, dropped[0].attrs)
	} else {
		require.Empty(t, logs.find("trigger rpc request conflict; event dropped", nil))
		require.Len(t, logs.find("trigger event delivered", map[string]string{"event_id": event.ID, "request_id": requestID}), 1)
	}
	require.Equal(t, triggerLedgerRecord{Total: 1, Entries: 1}, readTriggerLedger(ctx, harness, flow, flowID))
	require.Equal(t, int32(1), flow.lockedRuns.Load())

	// The same redelivery through a durable inbox is consumed on its first attempt in both cases.
	store := newTriggerInboxStore(t)
	durableTarget, err := newDurableTriggerTarget(store, "threadReplied", "ledger-changed", changedTarget)
	require.NoError(t, err)
	require.NoError(t, deliverAcknowledgedTrigger(ctx, durableTarget, event))
	require.Empty(t, pendingTriggerEventIDs(t, store))
	require.Equal(t, triggerLedgerRecord{Total: 1, Entries: 1}, readTriggerLedger(ctx, harness, flow, flowID))
	require.NotContains(t, logs.text(), sentinelText)
}

// TestDexRPCTriggerTargetAnswersARecordedRedeliveryToAClosedFlowWithRealDex: a closed run answers
// recorded request IDs for every RPC.
func TestDexRPCTriggerTargetAnswersARecordedRedeliveryToAClosedFlowWithRealDex(t *testing.T) {
	flow := &triggerLedgerFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := "trigger-ledger-closed-" + testRunID
	startTriggerLedger(ctx, t, harness, flow, flowID)
	lockedEvent := triggerApprovalEventFor("closed-locked-"+testRunID, testRunID)
	unlockedEvent := triggerApprovalEventFor("closed-unlocked-"+testRunID, testRunID)
	lockedTarget := newTriggerLedgerTarget(harness, flow.RecordLocked, flowID, 2)
	unlockedTarget := newTriggerLedgerTarget(harness, flow.RecordUnlocked, flowID, 3)
	require.NoError(t, lockedTarget.HandleTrigger(ctx, lockedEvent))
	require.NoError(t, unlockedTarget.HandleTrigger(ctx, unlockedEvent))
	require.Equal(t, triggerLedgerRecord{Total: 5, Entries: 2}, readTriggerLedger(ctx, harness, flow, flowID))
	require.NoError(t, harness.client.StopFlow(ctx, flowID, dex.StopOptions{Type: dex.TerminateFlow, Reason: "closed-Flow redelivery test"}))
	_, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)

	require.NoError(t, lockedTarget.HandleTrigger(ctx, lockedEvent))
	require.Equal(t, int32(1), flow.lockedRuns.Load(), "the locking RPC answered from the recorded reply")
	require.NoError(t, unlockedTarget.HandleTrigger(ctx, unlockedEvent))
	require.Equal(t, int32(2), flow.unlockedRuns.Load(), "the RPC without locks ran again on the closed run")

	err = lockedTarget.HandleTrigger(ctx, triggerApprovalEventFor("closed-new-"+testRunID, testRunID))
	require.True(t, sdkgo.IsTriggerUndeliverable(err), "an unrecorded event to a closed Flow: %v", err)
	require.Equal(t, int32(1), flow.lockedRuns.Load())
}

// TestDexRPCTriggerTargetAppliesAnEncryptedRedeliveryOnceWithRealDex: fresh nonces make servers before
// 0.1.37 report deterministic repeats as conflicts.
func TestDexRPCTriggerTargetAppliesAnEncryptedRedeliveryOnceWithRealDex(t *testing.T) {
	flow := &triggerLedgerFlow{}
	codec, err := dex.NewAESGCMPayloadCodec(staticPayloadEncryptionKeys{}, dex.AESGCMPayloadCodecOptions{})
	require.NoError(t, err)
	harness := newDexHarnessWithPayloadEncryption(t, []dex.Flow{flow}, dex.PayloadEncryptionOptions{Codec: codec})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := "trigger-ledger-encrypted-" + testRunID
	startTriggerLedger(ctx, t, harness, flow, flowID)
	logs := newLogRecorder(t)
	target := newTriggerLedgerTarget(harness, flow.RecordLocked, flowID, 3, sdkgo.WithTriggerLogger(logs.logger()))
	event := triggerApprovalEventFor("encrypted-"+testRunID, testRunID)
	require.NoError(t, target.HandleTrigger(ctx, event))

	err = target.HandleTrigger(ctx, event)
	if err != nil {
		require.True(t, sdkgo.IsTriggerUndeliverable(err), "a conflict is undeliverable: %v", err)
		var conflict *sdkgo.TriggerRPCRequestConflictError
		require.ErrorAs(t, err, &conflict)
		require.Contains(t, err.Error(), "Dex payload encryption on a Dex Server before 0.1.37")
		require.Len(t, logs.find("trigger rpc request conflict; event dropped", nil), 1)
	} else {
		require.Empty(t, logs.find("trigger rpc request conflict; event dropped", nil))
	}
	require.Equal(t, triggerLedgerRecord{Total: 3, Entries: 1}, readTriggerLedger(ctx, harness, flow, flowID))
	require.Equal(t, int32(1), flow.lockedRuns.Load())
	require.NotContains(t, logs.text(), sentinelText)
}

// staticPayloadEncryptionKeys serves one all-zero test key.
type staticPayloadEncryptionKeys struct{}

func (staticPayloadEncryptionKeys) CurrentKeyID(context.Context, dex.PayloadContext) (string, error) {
	return "test-key", nil
}

func (staticPayloadEncryptionKeys) Key(_ context.Context, _ dex.PayloadContext, keyID string) ([]byte, error) {
	if keyID != "test-key" {
		return nil, errors.New("unknown test payload encryption key")
	}
	return make([]byte, 32), nil
}
