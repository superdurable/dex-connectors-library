//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
	"google.golang.org/grpc/codes"
)

const blockedTriggerApprover = "blocked"

var triggerRejectionState = dex.DefineAttribute[triggerRejectionRecord]("trigger-rejection-state")

type triggerRejectionRecord struct {
	Status     string `json:"status"`
	ApprovedBy string `json:"approvedBy"`
}

type triggerRejectionInput struct {
	EventID string `json:"eventId"`
	UserID  string `json:"userId"`
}

type triggerRejectionEvent struct {
	ThreadID string `json:"threadId"`
	UserID   string `json:"userId"`
}

// triggerRejectionFlow rejects approvals from a blocked user by returning MarkTriggerUndeliverable from a locked RPC.
type triggerRejectionFlow struct {
	dex.FlowDefaults
}

func (*triggerRejectionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(triggerRejectionStartStep{})}
}

func (flow *triggerRejectionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.Approve, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(triggerRejectionState)}}),
		dex.DefineRPC(flow.GetRejectionState, nil),
	}
}

func (*triggerRejectionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{triggerRejectionState}}
}

func (*triggerRejectionFlow) Approve(ctx dex.Context, input triggerRejectionInput) (*dex.RPCResult[triggerRejectionRecord], error) {
	state, err := triggerRejectionState.Get(ctx)
	if err != nil {
		return nil, err
	}
	if input.UserID == blockedTriggerApprover {
		return nil, sdkgo.MarkTriggerUndeliverable(fmt.Errorf("approver %q is not allowed", input.UserID))
	}
	state.Status = triggerApprovalApproved
	state.ApprovedBy = input.UserID
	if err := triggerRejectionState.Set(ctx, state); err != nil {
		return nil, err
	}
	return &dex.RPCResult[triggerRejectionRecord]{Output: state}, nil
}

func (*triggerRejectionFlow) GetRejectionState(ctx dex.Context, _ dex.None) (*dex.RPCResult[triggerRejectionRecord], error) {
	state, err := triggerRejectionState.Get(ctx)
	var notFound *dex.AttributeNotFoundError
	if errors.As(err, &notFound) {
		return &dex.RPCResult[triggerRejectionRecord]{Output: triggerRejectionRecord{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[triggerRejectionRecord]{Output: state}, nil
}

type triggerRejectionStartStep struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (triggerRejectionStartStep) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triggerRejectionState)}}
}

func (triggerRejectionStartStep) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if err := triggerRejectionState.Set(ctx, triggerRejectionRecord{Status: triggerApprovalWaiting}); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// TestRPCHandlerMarksTriggerUndeliverableWithRealDex proves the handler's classification survives the Worker boundary.
func TestRPCHandlerMarksTriggerUndeliverableWithRealDex(t *testing.T) {
	flow := &triggerRejectionFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := "trigger-rejection-" + testRunID
	_, err := harness.client.StartFlow(ctx, flow, flowID, nil, dex.StartFlowOptions{})
	require.NoError(t, err)
	readState := func() triggerRejectionRecord {
		var state triggerRejectionRecord
		require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.GetRejectionState, nil, &state))
		return state
	}
	require.Eventually(t, func() bool {
		var state triggerRejectionRecord
		return harness.client.InvokeRPC(ctx, flowID, flow.GetRejectionState, nil, &state) == nil && state.Status == triggerApprovalWaiting
	}, 20*time.Second, 100*time.Millisecond)

	rawTarget := sdkgo.NewDexRPCTriggerTarget(harness.client, flow.Approve,
		func(sdkgo.TriggerEvent[triggerRejectionEvent]) bool { return true },
		func(sdkgo.TriggerEvent[triggerRejectionEvent]) string { return flowID },
		func(event sdkgo.TriggerEvent[triggerRejectionEvent]) triggerRejectionInput {
			return triggerRejectionInput{EventID: event.ID, UserID: event.Payload.UserID}
		})
	blocked := sdkgo.TriggerEvent[triggerRejectionEvent]{
		ID: "approve-blocked-" + testRunID, OccurredAt: time.Now().UTC(),
		Payload: triggerRejectionEvent{ThreadID: testRunID, UserID: blockedTriggerApprover},
	}
	require.Eventually(t, func() bool {
		err = rawTarget.HandleTrigger(ctx, blocked)
		var lockConflict *dex.RPCLockConflictError
		return !errors.As(err, &lockConflict)
	}, 20*time.Second, 100*time.Millisecond)
	require.True(t, sdkgo.IsTriggerUndeliverable(err), "RPC Trigger error: %v", err)
	require.ErrorContains(t, err, `approver "blocked" is not allowed`)
	var workerFailure *dex.WorkerInvocationError
	require.ErrorAs(t, err, &workerFailure)
	require.NotNil(t, workerFailure.Worker)
	require.Equal(t, codes.FailedPrecondition, workerFailure.Worker.Code)
	require.Equal(t, triggerRejectionRecord{Status: triggerApprovalWaiting}, readState())

	store, directory := newTriggerInboxStore(t)
	durableTarget, err := localconfig.NewDurableTriggerTarget(store, "fixture", "local", "threadReplied", "rejection-reply", rawTarget)
	require.NoError(t, err)
	blockedAgain := blocked
	blockedAgain.ID = "approve-blocked-again-" + testRunID
	require.NoError(t, sdkgo.PrepareTriggerDelivery(ctx, durableTarget, blockedAgain))
	require.Equal(t, []string{blockedAgain.ID}, pendingTriggerEventIDs(t, directory))
	require.Eventually(t, func() bool {
		return durableTarget.HandleTrigger(ctx, blockedAgain) == nil
	}, 20*time.Second, 100*time.Millisecond)
	require.Empty(t, pendingTriggerEventIDs(t, directory))
	require.Equal(t, triggerRejectionRecord{Status: triggerApprovalWaiting}, readState())
}

type heartbeatStartInput struct {
	EventID string `json:"eventId"`
}

// heartbeatFlow's start Step uses the Flow's heartbeat timeout, so one Flow type can be registered broken and fixed.
type heartbeatFlow struct {
	dex.FlowDefaults
	heartbeat time.Duration
}

func (flow *heartbeatFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(heartbeatStartStep{heartbeat: flow.heartbeat})}
}

func (*heartbeatFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

type heartbeatStartStep struct {
	dex.StepDefaultsNoWaitFor[heartbeatStartInput]
	heartbeat time.Duration
}

func (step heartbeatStartStep) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{HeartbeatTimeout: step.heartbeat}
}

func (heartbeatStartStep) Execute(_ dex.Context, input heartbeatStartInput) (*dex.StepDecision, error) {
	return dex.GracefulComplete(input.EventID), nil
}

// TestDexRejectedFlowStartKeepsEventUntilFlowIsFixedWithRealDex covers a Flow defect that only the Dex Server detects:
// a Step heartbeat below the server's minimum. The event stays pending and starts the fixed Flow after a restart.
func TestDexRejectedFlowStartKeepsEventUntilFlowIsFixedWithRealDex(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, directory := newTriggerInboxStore(t)
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := "trigger-heartbeat-" + testRunID
	newStartTarget := func(client *dex.Client, flow *heartbeatFlow) sdkgo.TriggerTarget[triggerApprovalEvent] {
		t.Helper()
		target, err := localconfig.NewDurableTriggerTarget(store, "fixture", "local", "threadCreated", "heartbeat-start",
			sdkgo.NewDexFlowTriggerTarget(client, flow,
				func(sdkgo.TriggerEvent[triggerApprovalEvent]) bool { return true },
				func(sdkgo.TriggerEvent[triggerApprovalEvent]) string { return flowID },
				func(event sdkgo.TriggerEvent[triggerApprovalEvent]) heartbeatStartInput {
					return heartbeatStartInput{EventID: event.ID}
				}))
		require.NoError(t, err)
		return target
	}
	root := triggerApprovalEventFor("root-heartbeat-"+testRunID, testRunID)

	broken := &heartbeatFlow{heartbeat: 5 * time.Second}
	brokenHarness := newDexHarness(t, []dex.Flow{broken})
	brokenHarness.startWorker(t)
	err := deliverAcknowledgedTrigger(ctx, newStartTarget(brokenHarness.client, broken), root)
	require.Error(t, err)
	require.False(t, sdkgo.IsTriggerUndeliverable(err), "a server-rejected Flow start must stay retryable: %v", err)
	var serviceError *dex.ServiceError
	require.ErrorAs(t, err, &serviceError)
	require.Equal(t, codes.InvalidArgument, serviceError.Code)
	require.ErrorContains(t, err, "heartbeat")
	require.Equal(t, []string{root.ID}, pendingTriggerEventIDs(t, directory))
	brokenHarness.stopWorker(t)

	fixed := &heartbeatFlow{}
	fixedHarness := newDexHarness(t, []dex.Flow{fixed})
	fixedHarness.startWorker(t)
	replayer, ok := newStartTarget(fixedHarness.client, fixed).(sdkgo.TriggerDeliveryReplayer)
	require.True(t, ok)
	require.NoError(t, replayer.ReplayTriggerDeliveries(ctx))
	require.Empty(t, pendingTriggerEventIDs(t, directory))
	result, err := fixedHarness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var startedBy string
	require.NoError(t, result.DecodeSingleOutput(&startedBy))
	require.Equal(t, root.ID, startedBy)
}

type relayInput struct {
	DownstreamFlowID string `json:"downstreamFlowId"`
}

// downstreamFlow's RPC fails until the test marks the downstream service available.
type downstreamFlow struct {
	dex.FlowDefaults
	available atomic.Bool
}

func (*downstreamFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(idleStartStep{})}
}

func (flow *downstreamFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.Forward, nil)}
}

func (*downstreamFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

func (flow *downstreamFlow) Forward(dex.Context, dex.None) (*dex.RPCResult[string], error) {
	if !flow.available.Load() {
		return nil, errors.New("downstream is temporarily unavailable")
	}
	return &dex.RPCResult[string]{Output: "forwarded"}, nil
}

// relayFlow's RPC calls downstreamFlow's RPC and returns its Dex error unchanged.
type relayFlow struct {
	dex.FlowDefaults
	downstream *downstreamFlow
	client     *dex.Client
}

func (*relayFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(idleStartStep{})}
}

func (flow *relayFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.Relay, nil)}
}

func (*relayFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

func (flow *relayFlow) Relay(ctx dex.Context, input relayInput) (*dex.RPCResult[string], error) {
	var output string
	if err := flow.client.InvokeRPC(ctx, input.DownstreamFlowID, flow.downstream.Forward, nil, &output); err != nil {
		return nil, err
	}
	return &dex.RPCResult[string]{Output: output}, nil
}

type idleStartStep struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (idleStartStep) Execute(dex.Context, dex.None) (*dex.StepDecision, error) {
	return dex.DeadEnd(), nil
}

// TestNestedWorkerFailureStaysRetryableWithRealDex covers an RPC handler that returns another Flow's Dex error.
// Its Worker code is FailedPrecondition, but it is not the handler's own marker, so the event stays pending.
func TestNestedWorkerFailureStaysRetryableWithRealDex(t *testing.T) {
	downstream := &downstreamFlow{}
	relay := &relayFlow{downstream: downstream}
	harness := newDexHarness(t, []dex.Flow{downstream, relay})
	relay.client = harness.client
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	downstreamID := "trigger-downstream-" + testRunID
	relayID := "trigger-relay-" + testRunID
	for flowID, flow := range map[string]dex.Flow{downstreamID: downstream, relayID: relay} {
		_, err := harness.client.StartFlow(ctx, flow, flowID, nil, dex.StartFlowOptions{})
		require.NoError(t, err)
	}
	store, directory := newTriggerInboxStore(t)
	target, err := localconfig.NewDurableTriggerTarget(store, "fixture", "local", "threadReplied", "relay-reply",
		sdkgo.NewDexRPCTriggerTarget(harness.client, relay.Relay,
			func(sdkgo.TriggerEvent[triggerApprovalEvent]) bool { return true },
			func(sdkgo.TriggerEvent[triggerApprovalEvent]) string { return relayID },
			func(sdkgo.TriggerEvent[triggerApprovalEvent]) relayInput {
				return relayInput{DownstreamFlowID: downstreamID}
			}))
	require.NoError(t, err)
	reply := triggerApprovalEventFor("reply-relay-"+testRunID, testRunID)
	require.NoError(t, sdkgo.PrepareTriggerDelivery(ctx, target, reply))

	var handleErr error
	var workerFailure *dex.WorkerInvocationError
	require.Eventually(t, func() bool {
		handleErr = target.HandleTrigger(ctx, reply)
		return errors.As(handleErr, &workerFailure) && workerFailure.Worker != nil
	}, 20*time.Second, 100*time.Millisecond, "last error: %v", handleErr)
	require.Equal(t, codes.FailedPrecondition, workerFailure.Worker.Code)
	require.ErrorContains(t, handleErr, "downstream is temporarily unavailable")
	require.False(t, sdkgo.IsTriggerUndeliverable(handleErr), "a nested Dex error must stay retryable: %v", handleErr)
	require.Equal(t, []string{reply.ID}, pendingTriggerEventIDs(t, directory))

	downstream.available.Store(true)
	require.NoError(t, sdkgo.DeliverTrigger(ctx, target, reply))
	require.Empty(t, pendingTriggerEventIDs(t, directory))
}

var undecodableRPCState = dex.DefineAttribute[int]("trigger-undecodable-rpc-count")

// undecodableOutput encodes on the Worker but fails to decode on the client, as a version skew would.
type undecodableOutput struct {
	Applied bool `json:"applied"`
}

func (*undecodableOutput) UnmarshalJSON([]byte) error {
	return errors.New("client cannot decode this output")
}

// undecodableResponseFlow counts applied RPCs and returns an output the client cannot decode.
type undecodableResponseFlow struct {
	dex.FlowDefaults
}

func (*undecodableResponseFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(undecodableResponseStartStep{})}
}

func (flow *undecodableResponseFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.Record, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(undecodableRPCState)}}),
		dex.DefineRPC(flow.GetCount, nil),
	}
}

func (*undecodableResponseFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{undecodableRPCState}}
}

func (*undecodableResponseFlow) Record(ctx dex.Context, _ dex.None) (*dex.RPCResult[undecodableOutput], error) {
	count, err := undecodableRPCState.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := undecodableRPCState.Set(ctx, count+1); err != nil {
		return nil, err
	}
	return &dex.RPCResult[undecodableOutput]{Output: undecodableOutput{Applied: true}}, nil
}

func (*undecodableResponseFlow) GetCount(ctx dex.Context, _ dex.None) (*dex.RPCResult[int], error) {
	count, err := undecodableRPCState.Get(ctx)
	var notFound *dex.AttributeNotFoundError
	if errors.As(err, &notFound) {
		return &dex.RPCResult[int]{Output: -1}, nil
	}
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[int]{Output: count}, nil
}

type undecodableResponseStartStep struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (undecodableResponseStartStep) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(undecodableRPCState)}}
}

func (undecodableResponseStartStep) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	if err := undecodableRPCState.Set(ctx, 0); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// TestRPCTriggerConsumesUndecodableResponseWithRealDex covers an RPC that Dex applied but whose response the
// client cannot decode. Retrying would apply it again, so the inbox consumes the event after one application.
func TestRPCTriggerConsumesUndecodableResponseWithRealDex(t *testing.T) {
	flow := &undecodableResponseFlow{}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := "trigger-undecodable-" + testRunID
	_, err := harness.client.StartFlow(ctx, flow, flowID, nil, dex.StartFlowOptions{})
	require.NoError(t, err)
	readCount := func() int {
		var count int
		require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.GetCount, nil, &count))
		return count
	}
	require.Eventually(t, func() bool {
		var count int
		return harness.client.InvokeRPC(ctx, flowID, flow.GetCount, nil, &count) == nil && count == 0
	}, 20*time.Second, 100*time.Millisecond)

	var output undecodableOutput
	rawErr := harness.client.InvokeRPC(ctx, flowID, flow.Record, nil, &output)
	var mappingFailure *dex.ValueMappingError
	require.ErrorAs(t, rawErr, &mappingFailure)
	require.Equal(t, "decode", mappingFailure.Operation)
	require.Equal(t, 1, readCount())

	store, directory := newTriggerInboxStore(t)
	target, err := localconfig.NewDurableTriggerTarget(store, "fixture", "local", "threadReplied", "undecodable-reply",
		sdkgo.NewDexRPCTriggerTarget(harness.client, flow.Record,
			func(sdkgo.TriggerEvent[triggerApprovalEvent]) bool { return true },
			func(sdkgo.TriggerEvent[triggerApprovalEvent]) string { return flowID },
			func(sdkgo.TriggerEvent[triggerApprovalEvent]) dex.None { return nil }))
	require.NoError(t, err)
	reply := triggerApprovalEventFor("reply-undecodable-"+testRunID, testRunID)
	require.NoError(t, deliverAcknowledgedTrigger(ctx, target, reply))
	require.Empty(t, pendingTriggerEventIDs(t, directory))
	require.Equal(t, 2, readCount())
}
