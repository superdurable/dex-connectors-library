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
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex-connectors-library/sdk/go/integrationtest/fixtureconnector"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	lookupWidgetStepType = "LookupWidget"
	createWidgetStepType = "CreateWidget"
)

var (
	createWidgetResult   = dex.DefineAttribute[connector.MutationResult[fixtureconnector.Widget]]("fixture-create-widget-result")
	createWidgetProgress = dex.DefineStream[connector.ProgressUpdate]("fixture-create-widget-progress", 1<<20)
	triggerStartEventID  = dex.DefineAttribute[string]("trigger-adapter-start-event-id")
	triggerApprovalCount = dex.DefineAttribute[int]("trigger-adapter-approval-count")
)

type flowInput struct {
	Name string `json:"name"`
}

type flowOutput struct {
	Widget         fixtureconnector.Widget  `json:"widget"`
	CallID         connector.CallID         `json:"callId"`
	IdempotencyKey connector.IdempotencyKey `json:"idempotencyKey"`
}

type connectorConsumerFlow struct {
	dex.FlowDefaults
	connection fixtureconnector.Connection
}

type lookupOutput = fixtureconnector.LookupWidgetStepOutput[flowInput]
type createOutput = fixtureconnector.CreateWidgetStepOutput[lookupOutput]

func (flow connectorConsumerFlow) GetSteps() []dex.StepDef {
	lookup := fixtureconnector.NewLookupWidgetStep(fixtureconnector.LookupWidgetStepConfig[flowInput]{
		StepType: lookupWidgetStepType,
		Presentation: connector.StepPresentation{
			GroupID: "fixture", GroupLabel: "Fixture", Explanation: "Look up the widget.",
		},
		Connection: flow.connection,
		BuildInput: func(input flowInput) (fixtureconnector.LookupInput, error) {
			return fixtureconnector.LookupInput{Name: input.Name}, nil
		},
		Found:  connector.GoTo(widgetAlreadyExistsStep{}),
		Absent: connector.GoTo(connector.StepRef[lookupOutput](createWidgetStepType)),
		Failed: connector.GoTo(connectorConsumerFailedStep[lookupOutput]{}),
		Defect: connector.GoTo(connectorConsumerFailedStep[lookupOutput]{}),
	})
	create := fixtureconnector.NewCreateWidgetStep(fixtureconnector.CreateWidgetStepConfig[lookupOutput]{
		StepType: createWidgetStepType,
		Presentation: connector.StepPresentation{
			GroupID: "fixture", GroupLabel: "Fixture", Explanation: "Create the missing widget.",
		},
		Connection: flow.connection,
		BuildInput: func(output lookupOutput) (fixtureconnector.CreateInput, error) {
			return fixtureconnector.CreateInput{Name: output.Input.Name}, nil
		},
		Completed:       connector.GoTo(widgetCreatedStep{}),
		Rejected:        connector.GoTo(connectorConsumerFailedStep[createOutput]{}),
		Uncertain:       connector.GoTo(connectorConsumerFailedStep[createOutput]{}),
		Defect:          connector.GoTo(connectorConsumerFailedStep[createOutput]{}),
		ResultAttribute: &createWidgetResult,
		ProgressStream:  &createWidgetProgress,
	})
	return []dex.StepDef{
		dex.DefineStartStep(lookup),
		dex.DefineStep(create),
		dex.DefineStep(widgetAlreadyExistsStep{}),
		dex.DefineStep(widgetCreatedStep{}),
		dex.DefineStep(connectorConsumerFailedStep[lookupOutput]{}),
		dex.DefineStep(connectorConsumerFailedStep[createOutput]{}),
	}
}

func (connectorConsumerFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{createWidgetResult},
		Streams:    []dex.StreamDef{createWidgetProgress},
	}
}

type widgetAlreadyExistsStep struct {
	dex.StepDefaultsNoWaitFor[lookupOutput]
}

func (widgetAlreadyExistsStep) Execute(_ dex.Context, output lookupOutput) (*dex.StepDecision, error) {
	return dex.GracefulComplete(flowOutput{Widget: output.Result.Value, CallID: output.Result.Receipt.CallID}), nil
}

type widgetCreatedStep struct {
	dex.StepDefaultsNoWaitFor[createOutput]
}

func (widgetCreatedStep) Execute(ctx dex.Context, output createOutput) (*dex.StepDecision, error) {
	persisted, err := createWidgetResult.Get(ctx)
	if err != nil {
		return nil, err
	}
	if persisted.Receipt.CallID != output.Result.Receipt.CallID {
		return dex.ForceFail("persisted connector result does not match Step output"), nil
	}
	return dex.GracefulComplete(flowOutput{
		Widget: output.Result.Value, CallID: output.Result.Receipt.CallID,
		IdempotencyKey: output.Result.Receipt.IdempotencyKey,
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
	connection, err := fixtureconnector.NewConnection(provider, "integration", connector.NewSecretString("fixture-secret"))
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
	require.Equal(t, connector.IdempotencyKey(output.CallID), output.IdempotencyKey)

	stats := provider.Stats()
	require.Equal(t, 1, stats.QueryCount)
	require.Equal(t, 1, stats.WriteCount)
	require.Len(t, stats.MutationCalls, 2)
	require.Equal(t, stats.MutationCalls[0], stats.MutationCalls[1])
	require.Equal(t, stats.IdempotencyKeys[0], stats.IdempotencyKeys[1])

	var page dex.StreamMessagesPage[connector.ProgressUpdate]
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

type triggerAdapterState struct {
	StartEventID  string `json:"startEventId"`
	ApprovalCount int    `json:"approvalCount"`
}

type triggerAdapterFlow struct {
	dex.FlowDefaults
	approveTriggerRPC *connector.TriggerRPC[triggerAdapterEvent, triggerAdapterState]
}

func newTriggerAdapterFlow() *triggerAdapterFlow {
	flow := &triggerAdapterFlow{}
	flow.approveTriggerRPC = connector.MustNewTriggerRPC(connector.TriggerRPCConfig[triggerAdapterEvent, triggerAdapterState]{
		Definition: flow.ApproveRequest, ProcessedEventIDsAttributeName: "trigger-adapter-processed-event-ids",
		HandleEvent: flow.handleApprovalEvent,
		Options: &dex.RPCOptions{LockAttributes: []dex.AttributeLock{
			dex.LockAttribute(triggerApprovalCount), dex.LockAttribute(triggerStartEventID),
		}},
	})
	return flow
}

func (*triggerAdapterFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(triggerAdapterStartStep{})}
}

func (flow *triggerAdapterFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.approveTriggerRPC.Definition(), flow.approveTriggerRPC.DefaultOptions()),
		dex.DefineRPC(flow.GetTriggerAdapterState, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{
			dex.LockAttribute(triggerStartEventID), dex.LockAttribute(triggerApprovalCount),
		}}),
	}
}

func (flow *triggerAdapterFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{
		triggerStartEventID, triggerApprovalCount, flow.approveTriggerRPC.PersistenceAttribute(),
	}}
}

func (flow *triggerAdapterFlow) ApproveRequest(
	ctx dex.Context,
	event connector.TriggerEvent[triggerAdapterEvent],
) (*dex.RPCResult[triggerAdapterState], error) {
	return flow.approveTriggerRPC.Handle(ctx, event)
}

func (*triggerAdapterFlow) handleApprovalEvent(
	ctx dex.Context,
	_ connector.TriggerEvent[triggerAdapterEvent],
) (*dex.RPCResult[triggerAdapterState], error) {
	approvalCount, err := triggerApprovalCount.Get(ctx)
	var notFound *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &notFound) {
		return nil, err
	}
	approvalCount++
	if err := triggerApprovalCount.Set(ctx, approvalCount); err != nil {
		return nil, err
	}
	startEventID, err := triggerStartEventID.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[triggerAdapterState]{Output: triggerAdapterState{
		StartEventID: startEventID, ApprovalCount: approvalCount,
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
	approvalCount, err := triggerApprovalCount.Get(ctx)
	var notFound *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &notFound) {
		return nil, err
	}
	return &dex.RPCResult[triggerAdapterState]{Output: triggerAdapterState{
		StartEventID: startEventID, ApprovalCount: approvalCount,
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

func TestTriggerTargetsResolveFlowAndDeduplicateRPCEventsWithRealDex(t *testing.T) {
	flow := newTriggerAdapterFlow()
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	threadID := "thread-" + testRunID
	resolveFlowID := func(event connector.TriggerEvent[triggerAdapterEvent]) (string, error) {
		return "connector-trigger-" + event.Payload.ThreadID, nil
	}
	flowID := "connector-trigger-" + threadID
	startTarget := connector.NewDexFlowTriggerTarget(harness.client, flow, resolveFlowID,
		func(event connector.TriggerEvent[triggerAdapterEvent]) (triggerAdapterInput, error) {
			return triggerAdapterInput{EventID: event.ID}, nil
		})
	rootEventID := "root-event-" + testRunID
	rootEvent := connector.TriggerEvent[triggerAdapterEvent]{ID: rootEventID, Payload: triggerAdapterEvent{ThreadID: threadID}}
	require.NoError(t, startTarget.HandleTrigger(ctx, rootEvent))
	require.NoError(t, startTarget.HandleTrigger(ctx, rootEvent))

	replyTarget := connector.NewDexRPCTriggerTarget(harness.client, flow.approveTriggerRPC.Definition(), resolveFlowID)
	replyEvent := connector.TriggerEvent[triggerAdapterEvent]{ID: "reply-event-" + testRunID, Payload: triggerAdapterEvent{ThreadID: threadID}}
	require.Eventually(t, func() bool {
		return replyTarget.HandleTrigger(ctx, replyEvent) == nil
	}, 20*time.Second, 100*time.Millisecond)
	require.NoError(t, replyTarget.HandleTrigger(ctx, replyEvent))

	var state triggerAdapterState
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.GetTriggerAdapterState, nil, &state))
	require.Equal(t, triggerAdapterState{StartEventID: rootEventID, ApprovalCount: 1}, state)
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
