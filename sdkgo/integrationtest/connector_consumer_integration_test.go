//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/integrationtest/fixtureconnector"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	lookupWidgetStepType = "LookupWidget"
	createWidgetStepType = "CreateWidget"
)

var (
	connectorConsumerInput = dex.DefineAttribute[flowInput]("connector-consumer-input")
	createWidgetResult     = dex.DefineAttribute[sdkgo.MutationResult[fixtureconnector.Widget]]("fixture-create-widget-result")
	createWidgetProgress   = dex.DefineStream[sdkgo.ProgressUpdate]("fixture-create-widget-progress", 1<<20)
	triggerStartEventID    = dex.DefineAttribute[string]("trigger-adapter-start-event-id")
	triggerApprovalEventID = dex.DefineAttribute[string]("trigger-adapter-approval-event-id")
	triggerApprovalCount   = dex.DefineAttribute[int]("trigger-adapter-approval-count")
)

type flowInput struct {
	Name string `json:"name"`
}

type flowOutput struct {
	Widget         fixtureconnector.Widget `json:"widget"`
	CallID         sdkgo.CallID            `json:"callId"`
	IdempotencyKey sdkgo.IdempotencyKey    `json:"idempotencyKey"`
}

type connectorConsumerFlow struct {
	dex.FlowDefaults
	connection fixtureconnector.Connection
}

type lookupResult = fixtureconnector.LookupWidgetResult
type createResult = fixtureconnector.CreateWidgetResult

func (flow connectorConsumerFlow) GetSteps() []dex.StepDef {
	lookup := fixtureconnector.NewLookupWidgetStep(fixtureconnector.LookupWidgetStepConfig[flowInput]{
		StepType: lookupWidgetStepType,
		Annotations: sdkgo.StepAnnotations{
			GroupID: "fixture", GroupLabel: "Fixture", Explanation: "Look up the widget.",
		},
		Connection: flow.connection,
		MapToOperationInput: func(input flowInput) fixtureconnector.LookupInput {
			return fixtureconnector.LookupInput{Name: input.Name}
		},
		Found:  sdkgo.GoTo(widgetAlreadyExistsStep{}),
		Absent: sdkgo.GoTo(prepareWidgetCreationStep{}),
		Failed: sdkgo.GoTo(connectorConsumerFailedStep[lookupResult]{}),
		Defect: sdkgo.GoTo(connectorConsumerFailedStep[lookupResult]{}),
	})
	create := fixtureconnector.NewCreateWidgetStep(fixtureconnector.CreateWidgetStepConfig[flowInput]{
		StepType: createWidgetStepType,
		Annotations: sdkgo.StepAnnotations{
			GroupID: "fixture", GroupLabel: "Fixture", Explanation: "Create the missing widget.",
		},
		Connection: flow.connection,
		MapToOperationInput: func(input flowInput) fixtureconnector.CreateInput {
			return fixtureconnector.CreateInput{Name: input.Name}
		},
		Completed:       sdkgo.GoTo(widgetCreatedStep{}),
		Rejected:        sdkgo.GoTo(connectorConsumerFailedStep[createResult]{}),
		Uncertain:       sdkgo.GoTo(connectorConsumerFailedStep[createResult]{}),
		Defect:          sdkgo.GoTo(connectorConsumerFailedStep[createResult]{}),
		ResultAttribute: &createWidgetResult,
		ProgressStream:  &createWidgetProgress,
	})
	return []dex.StepDef{
		dex.DefineStartStep(initializeConnectorConsumerStep{}),
		dex.DefineStep(lookup),
		dex.DefineStep(create),
		dex.DefineStep(prepareWidgetCreationStep{}),
		dex.DefineStep(widgetAlreadyExistsStep{}),
		dex.DefineStep(widgetCreatedStep{}),
		dex.DefineStep(connectorConsumerFailedStep[lookupResult]{}),
		dex.DefineStep(connectorConsumerFailedStep[createResult]{}),
	}
}

func (connectorConsumerFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{connectorConsumerInput, createWidgetResult},
		Streams:    []dex.StreamDef{createWidgetProgress},
	}
}

type initializeConnectorConsumerStep struct {
	dex.StepDefaultsNoWaitFor[flowInput]
}

func (initializeConnectorConsumerStep) Execute(ctx dex.Context, input flowInput) (*dex.StepDecision, error) {
	if err := connectorConsumerInput.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[flowInput](lookupWidgetStepType), input), nil
}

type widgetAlreadyExistsStep struct {
	dex.StepDefaultsNoWaitFor[lookupResult]
}

func (widgetAlreadyExistsStep) Execute(_ dex.Context, result lookupResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(flowOutput{Widget: result.Value, CallID: result.Receipt.CallID}), nil
}

type prepareWidgetCreationStep struct {
	dex.StepDefaultsNoWaitFor[lookupResult]
}

func (prepareWidgetCreationStep) Execute(ctx dex.Context, _ lookupResult) (*dex.StepDecision, error) {
	input, err := connectorConsumerInput.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[flowInput](createWidgetStepType), input), nil
}

type widgetCreatedStep struct {
	dex.StepDefaultsNoWaitFor[createResult]
}

func (widgetCreatedStep) Execute(ctx dex.Context, result createResult) (*dex.StepDecision, error) {
	persisted, err := createWidgetResult.Get(ctx)
	if err != nil {
		return nil, err
	}
	if persisted.Receipt.CallID != result.Receipt.CallID {
		return dex.ForceFail("persisted connector result does not match Step output"), nil
	}
	return dex.GracefulComplete(flowOutput{
		Widget: result.Value, CallID: result.Receipt.CallID,
		IdempotencyKey: result.Receipt.IdempotencyKey,
	}), nil
}

type connectorConsumerFailedStep[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (connectorConsumerFailedStep[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.ForceFail("fixture connector selected a failure branch"), nil
}

func TestConnectorModuleConsumesSDKFactoriesWithRealDex(t *testing.T) {
	provider := fixtureconnector.NewProvider()
	connection, err := fixtureconnector.NewConnection(provider, "integration", sdkgo.NewSecretString("fixture-secret"))
	require.NoError(t, err)
	flow := connectorConsumerFlow{connection: connection}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := fmt.Sprintf("sdk-connector-consumer-%d", time.Now().UnixNano())
	_, err = harness.client.StartFlow(ctx, flow, flowID, flowInput{Name: "alpha"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	var output flowOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, fixtureconnector.Widget{ID: "widget-1", Name: "alpha"}, output.Widget)
	require.NotEmpty(t, output.CallID)
	require.Equal(t, sdkgo.IdempotencyKey(output.CallID), output.IdempotencyKey)

	stats := provider.Stats()
	require.Equal(t, 1, stats.QueryCount)
	require.Equal(t, 1, stats.WriteCount)
	require.Len(t, stats.MutationCalls, 2)
	require.Equal(t, stats.MutationCalls[0], stats.MutationCalls[1])
	require.Equal(t, stats.IdempotencyKeys[0], stats.IdempotencyKeys[1])

	var page dex.StreamMessagesPage[sdkgo.ProgressUpdate]
	require.NoError(t, harness.client.ListStreamMessages(ctx, flowID, createWidgetProgress, 10, "", &page))
	require.Len(t, page.Messages, 2)
	require.Equal(t, output.CallID, page.Messages[0].Value.CallID)
	require.Equal(t, output.CallID, page.Messages[1].Value.CallID)
	require.Equal(t, []int32{2, 1}, []int32{page.Messages[0].Value.Attempt, page.Messages[1].Value.Attempt})
	require.Equal(t, uint64(1), page.Messages[0].Value.Sequence)
	require.Equal(t, uint64(1), page.Messages[1].Value.Sequence)
}

type triggerAdapterEvent struct {
	ThreadID string `json:"threadId"`
}

type triggerAdapterInput struct {
	EventID string `json:"eventId"`
}

type triggerAdapterApprovalInput struct {
	EventID string `json:"eventId"`
}

type triggerAdapterState struct {
	StartEventID    string `json:"startEventId"`
	ApprovalEventID string `json:"approvalEventId"`
	ApprovalCount   int    `json:"approvalCount"`
}

type triggerAdapterFlow struct {
	dex.FlowDefaults
}

func newTriggerAdapterFlow() *triggerAdapterFlow {
	return &triggerAdapterFlow{}
}

func (*triggerAdapterFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(triggerAdapterStartStep{})}
}

func (flow *triggerAdapterFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.ApproveRequest, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{
			dex.LockAttribute(triggerStartEventID), dex.LockAttribute(triggerApprovalEventID),
			dex.LockAttribute(triggerApprovalCount),
		}}),
		dex.DefineRPC(flow.GetTriggerAdapterState, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{
			dex.LockAttribute(triggerStartEventID), dex.LockAttribute(triggerApprovalEventID),
			dex.LockAttribute(triggerApprovalCount),
		}}),
	}
}

func (flow *triggerAdapterFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{
		triggerStartEventID, triggerApprovalEventID, triggerApprovalCount,
	}}
}

func (flow *triggerAdapterFlow) ApproveRequest(
	ctx dex.Context,
	input triggerAdapterApprovalInput,
) (*dex.RPCResult[triggerAdapterState], error) {
	if input.EventID == "" {
		return nil, fmt.Errorf("approval event ID is required")
	}
	approvalEventID, err := triggerApprovalEventID.Get(ctx)
	var notFound *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &notFound) {
		return nil, err
	}
	if approvalEventID != "" {
		return flow.GetTriggerAdapterState(ctx, nil)
	}
	approvalCount, err := triggerApprovalCount.Get(ctx)
	if err != nil && !errors.As(err, &notFound) {
		return nil, err
	}
	approvalCount++
	if err := triggerApprovalEventID.Set(ctx, input.EventID); err != nil {
		return nil, err
	}
	if err := triggerApprovalCount.Set(ctx, approvalCount); err != nil {
		return nil, err
	}
	startEventID, err := triggerStartEventID.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[triggerAdapterState]{Output: triggerAdapterState{
		StartEventID: startEventID, ApprovalEventID: input.EventID, ApprovalCount: approvalCount,
	}}, nil
}

func (*triggerAdapterFlow) GetTriggerAdapterState(
	ctx dex.Context,
	_ dex.None,
) (*dex.RPCResult[triggerAdapterState], error) {
	startEventID, err := triggerStartEventID.Get(ctx)
	if err != nil {
		return nil, err
	}
	approvalEventID, err := triggerApprovalEventID.Get(ctx)
	var notFound *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &notFound) {
		return nil, err
	}
	approvalCount, err := triggerApprovalCount.Get(ctx)
	if err != nil && !errors.As(err, &notFound) {
		return nil, err
	}
	return &dex.RPCResult[triggerAdapterState]{Output: triggerAdapterState{
		StartEventID: startEventID, ApprovalEventID: approvalEventID, ApprovalCount: approvalCount,
	}}, nil
}

type triggerAdapterStartStep struct {
	dex.StepDefaultsNoWaitFor[triggerAdapterInput]
}

func (triggerAdapterStartStep) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triggerStartEventID)}}
}

func (triggerAdapterStartStep) Execute(ctx dex.Context, input triggerAdapterInput) (*dex.StepDecision, error) {
	if err := triggerStartEventID.Set(ctx, input.EventID); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

func TestTriggerTargetsResolveFlowAndApplicationRPCOwnsDeduplicationWithRealDex(t *testing.T) {
	flow := newTriggerAdapterFlow()
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	threadID := "thread-" + testRunID
	resolveFlowID := func(event sdkgo.TriggerEvent[triggerAdapterEvent]) string {
		return "connector-trigger-" + event.Payload.ThreadID
	}
	flowID := "connector-trigger-" + threadID
	filterEvent := func(event sdkgo.TriggerEvent[triggerAdapterEvent]) bool {
		return event.Payload.ThreadID == threadID
	}
	startTarget := sdkgo.NewDexFlowTriggerTarget(harness.client, flow, filterEvent, resolveFlowID,
		func(event sdkgo.TriggerEvent[triggerAdapterEvent]) triggerAdapterInput {
			return triggerAdapterInput{EventID: event.ID}
		})
	rejectedEvent := sdkgo.TriggerEvent[triggerAdapterEvent]{ID: "rejected-" + testRunID, Payload: triggerAdapterEvent{ThreadID: "other-thread"}}
	require.NoError(t, startTarget.HandleTrigger(ctx, rejectedEvent))
	rootEventID := "root-event-" + testRunID
	rootEvent := sdkgo.TriggerEvent[triggerAdapterEvent]{ID: rootEventID, Payload: triggerAdapterEvent{ThreadID: threadID}}
	require.NoError(t, startTarget.HandleTrigger(ctx, rootEvent))
	require.NoError(t, startTarget.HandleTrigger(ctx, rootEvent))

	replyTarget := sdkgo.NewDexRPCTriggerTarget(harness.client, flow.ApproveRequest, filterEvent, resolveFlowID,
		func(event sdkgo.TriggerEvent[triggerAdapterEvent]) triggerAdapterApprovalInput {
			return triggerAdapterApprovalInput{EventID: event.ID}
		})
	require.NoError(t, replyTarget.HandleTrigger(ctx, rejectedEvent))
	replyEventID := "reply-event-" + testRunID
	replyEvent := sdkgo.TriggerEvent[triggerAdapterEvent]{ID: replyEventID, Payload: triggerAdapterEvent{ThreadID: threadID}}
	require.Eventually(t, func() bool {
		return replyTarget.HandleTrigger(ctx, replyEvent) == nil
	}, 20*time.Second, 100*time.Millisecond)
	require.NoError(t, replyTarget.HandleTrigger(ctx, replyEvent))

	var state triggerAdapterState
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.GetTriggerAdapterState, nil, &state))
	require.Equal(t, triggerAdapterState{StartEventID: rootEventID, ApprovalEventID: replyEventID, ApprovalCount: 1}, state)
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
	workerAddress := net.JoinHostPort("127.0.0.1", availablePort(t))
	harness := &dexHarness{
		registry: registry, cache: cache, workerAddress: workerAddress,
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress,
		WorkerTarget:       &dex.WorkerTarget{Address: workerAddress},
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
