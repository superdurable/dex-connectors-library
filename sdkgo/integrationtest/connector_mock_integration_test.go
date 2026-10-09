//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/connectormock"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	mockedInventoryFound   sdkgo.BranchID = "found"
	mockedInventoryMissing sdkgo.BranchID = "missing"
	mockedOrderPlaced      sdkgo.BranchID = "placed"
)

var (
	mockedInventoryConnection = sdkgo.ConnectionRef{Provider: "inventory", Name: "warehouse"}
	checkStockDefinition      = sdkgo.QueryDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "inventory", OperationID: "checkStock"},
		Branches: []sdkgo.BranchDefinition{
			{ID: mockedInventoryFound, Description: "The item is in stock."},
			{ID: mockedInventoryMissing, Description: "The item is out of stock.", Optional: true},
			{ID: sdkgo.DefectBranchID, Description: "The input is invalid.", Optional: true},
		},
		StepDefaults: sdkgo.StepDefaults{
			ExecuteMethodTimeout: 10 * time.Second,
			ExecuteRetry:         &dex.RetryPolicy{MaximumAttempts: 3, InitialInterval: 10 * time.Millisecond},
			ExecuteDurability:    dex.StepDurabilityAsync,
		},
	}
	placeOrderDefinition = sdkgo.MutationDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "inventory", OperationID: "placeOrder"},
		Branches: []sdkgo.BranchDefinition{
			{ID: mockedOrderPlaced, Description: "The order was placed."},
			{ID: sdkgo.UncertainBranchID, Description: "The order outcome is unknown.", Optional: true},
			{ID: sdkgo.DefectBranchID, Description: "The input is invalid.", Optional: true},
		},
	}
)

type stockRequest struct {
	SKU string `json:"sku"`
}

type stockLevel struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

type orderReceipt struct {
	OrderID string `json:"orderId"`
}

// mockedStockFlow runs one generic Query Step whose operation is a connectormock.Query.
type mockedStockFlow struct {
	dex.FlowDefaults
	checkStock *connectormock.Query[stockRequest, stockLevel]
}

func (flow mockedStockFlow) GetSteps() []dex.StepDef {
	check := sdkgo.MustNewQueryStep(sdkgo.QueryStepConfig[stockRequest, stockRequest, stockLevel]{
		StepType:            "CheckMockedStock",
		Annotations:         sdkgo.StepAnnotations{GroupID: "inventory", GroupLabel: "Inventory", Explanation: "Check the item's stock."},
		Operation:           flow.checkStock,
		Connection:          mockedInventoryConnection,
		MapToOperationInput: func(request stockRequest) stockRequest { return request },
		Branches: []sdkgo.BranchTarget[sdkgo.QueryResult[stockLevel]]{
			sdkgo.GoToBranch(mockedInventoryFound, stockFoundStep{}),
			sdkgo.GoToBranch(mockedInventoryMissing, stockMissingStep{}),
		},
	})
	return []dex.StepDef{
		dex.DefineStartStep(check),
		dex.DefineStep(stockFoundStep{}),
		dex.DefineStep(stockMissingStep{}),
	}
}

func (mockedStockFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

type stockFoundStep struct {
	dex.StepDefaultsNoWaitFor[sdkgo.QueryResult[stockLevel]]
}

func (stockFoundStep) Execute(_ dex.Context, result sdkgo.QueryResult[stockLevel]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

type stockMissingStep struct {
	dex.StepDefaultsNoWaitFor[sdkgo.QueryResult[stockLevel]]
}

func (stockMissingStep) Execute(dex.Context, sdkgo.QueryResult[stockLevel]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(stockLevel{SKU: "missing"}), nil
}

// mockedOrderFlow places an order and loses the Step's first response after the provider call, so Dex retries
// Execute within the same Step execution.
type mockedOrderFlow struct {
	dex.FlowDefaults
	placeOrder *connectormock.Mutation[stockRequest, orderReceipt]
}

func (flow mockedOrderFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(placeOrderLosingFirstResponseStep{placeOrder: flow.placeOrder})}
}

func (mockedOrderFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

type placeOrderLosingFirstResponseStep struct {
	dex.StepDefaultsNoWaitFor[stockRequest]
	placeOrder *connectormock.Mutation[stockRequest, orderReceipt]
}

func (placeOrderLosingFirstResponseStep) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{
		ExecuteMethodTimeout: 10 * time.Second,
		ExecuteRetry:         &dex.RetryPolicy{MaximumAttempts: 3, InitialInterval: 10 * time.Millisecond},
	}
}

func (step placeOrderLosingFirstResponseStep) Execute(ctx dex.Context, request stockRequest) (*dex.StepDecision, error) {
	result, err := sdkgo.RunMutation(ctx, step.placeOrder, mockedInventoryConnection, request)
	if err != nil {
		return nil, err
	}
	if ctx.Attempt() == 1 {
		return nil, errors.New("the Worker lost the response after the provider call")
	}
	return dex.GracefulComplete(fmt.Sprintf("%s:%s", result.Branch, result.Value.OrderID)), nil
}

func TestConnectorMockRetryTakesTheNextCaseInARealDexRetry(t *testing.T) {
	checkStock := connectormock.NewQuery[stockRequest, stockLevel](t, checkStockDefinition)
	checkStock.Respond(
		connectormock.Retry[stockLevel](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Message: "warehouse busy"}, 0),
		connectormock.Branch(mockedInventoryFound, stockLevel{SKU: "sku-1", Quantity: 7}, nil),
	)
	flow := mockedStockFlow{checkStock: checkStock}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	flowID := fmt.Sprintf("mocked-stock-retry-%d", time.Now().UnixNano())

	result := startAndWaitForMockedFlow(t, harness, flow, flowID, stockRequest{SKU: "sku-1"})

	require.Equal(t, dex.FlowCompleted, result.Status)
	var level stockLevel
	require.NoError(t, result.DecodeSingleOutput(&level))
	require.Equal(t, stockLevel{SKU: "sku-1", Quantity: 7}, level)
	calls := checkStock.Calls()
	require.Len(t, calls, 2)
	require.True(t, calls[0].IsRetry)
	require.Equal(t, mockedInventoryFound, calls[1].Branch)
	require.Equal(t, calls[0].CallID, calls[1].CallID, "a Dex retry keeps the Step execution's Call ID")
	require.Equal(t, flowID, calls[1].FlowID)
	require.Equal(t, stockRequest{SKU: "sku-1"}, calls[1].Input)
}

func TestConnectorMockReplaysARepeatedIdempotencyKeyAcrossARealDexRetry(t *testing.T) {
	placeOrder := connectormock.NewMutation[stockRequest, orderReceipt](t, placeOrderDefinition)
	placeOrder.Respond(
		connectormock.Branch(mockedOrderPlaced, orderReceipt{OrderID: "order-1"}, nil),
		connectormock.Branch(mockedOrderPlaced, orderReceipt{OrderID: "order-2"}, nil),
	)
	flow := mockedOrderFlow{placeOrder: placeOrder}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	flowID := fmt.Sprintf("mocked-order-replay-%d", time.Now().UnixNano())

	result := startAndWaitForMockedFlow(t, harness, flow, flowID, stockRequest{SKU: "sku-1"})

	require.Equal(t, dex.FlowCompleted, result.Status)
	var output string
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "placed:order-1", output)
	calls := placeOrder.Calls()
	require.Len(t, calls, 2)
	require.False(t, calls[0].IsReplay)
	require.True(t, calls[1].IsReplay)
	require.Equal(t, calls[0].IdempotencyKey, calls[1].IdempotencyKey)
}

func TestConnectorMockUnscriptedCallFailsTheTestAndTheFlowTakesDefect(t *testing.T) {
	recorder := &mockFailureRecorder{TB: t}
	checkStock := connectormock.NewQuery[stockRequest, stockLevel](recorder, checkStockDefinition)
	flow := mockedStockFlow{checkStock: checkStock}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)
	flowID := fmt.Sprintf("mocked-stock-unscripted-%d", time.Now().UnixNano())

	result := startAndWaitForMockedFlow(t, harness, flow, flowID, stockRequest{SKU: "sku-1"})

	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, `connector branch "defect" has no target`)
	require.Contains(t, recorder.messages(), "unscripted call for Flow")
	recorded := make([]connectormock.RecordedCall, 0, 1)
	for _, call := range connectormock.RecordedCalls() {
		if call.FlowID == flowID {
			recorded = append(recorded, call)
		}
	}
	require.Len(t, recorded, 1)
	require.Equal(t, sdkgo.DefectBranchID, recorded[0].Branch)
	require.True(t, recorded[0].IsUnscripted)
	require.Equal(t, "warehouse", recorded[0].ConnectionName)
}

func TestConnectorMockForFlowGivesConcurrentFlowsTheirOwnAnswers(t *testing.T) {
	checkStock := connectormock.NewQuery[stockRequest, stockLevel](t, checkStockDefinition)
	runID := time.Now().UnixNano()
	inStockFlowID := fmt.Sprintf("mocked-stock-in-%d", runID)
	outOfStockFlowID := fmt.Sprintf("mocked-stock-out-%d", runID)
	checkStock.ForFlow(inStockFlowID).Respond(connectormock.Branch(mockedInventoryFound, stockLevel{SKU: "sku-in", Quantity: 3}, nil))
	checkStock.ForFlow(outOfStockFlowID).Respond(connectormock.Branch(mockedInventoryMissing, stockLevel{}, nil))
	flow := mockedStockFlow{checkStock: checkStock}
	harness := newDexHarness(t, []dex.Flow{flow})
	harness.startWorker(t)

	outputs := make(map[string]stockLevel, 2)
	var mu sync.Mutex
	var group sync.WaitGroup
	for _, flowID := range []string{inStockFlowID, outOfStockFlowID} {
		group.Add(1)
		go func() {
			defer group.Done()
			result := startAndWaitForMockedFlow(t, harness, flow, flowID, stockRequest{SKU: flowID})
			var level stockLevel
			if result.Status == dex.FlowCompleted && result.DecodeSingleOutput(&level) == nil {
				mu.Lock()
				outputs[flowID] = level
				mu.Unlock()
			}
		}()
	}
	group.Wait()

	require.Equal(t, map[string]stockLevel{
		inStockFlowID:    {SKU: "sku-in", Quantity: 3},
		outOfStockFlowID: {SKU: "missing"},
	}, outputs)
}

func startAndWaitForMockedFlow(t *testing.T, harness *dexHarness, flow dex.Flow, flowID string, input stockRequest) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{}); err != nil {
		t.Errorf("start Flow %s: %v", flowID, err)
		return dex.FlowResult{}
	}
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	if err != nil {
		t.Errorf("wait for Flow %s: %v", flowID, err)
		return dex.FlowResult{}
	}
	return result
}

// mockFailureRecorder records the failures a mock reports instead of failing the test.
type mockFailureRecorder struct {
	testing.TB
	mu       sync.Mutex
	failures []string
}

func (recorder *mockFailureRecorder) Helper() {}

func (recorder *mockFailureRecorder) Errorf(format string, values ...any) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.failures = append(recorder.failures, fmt.Sprintf(format, values...))
}

func (recorder *mockFailureRecorder) messages() string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return strings.Join(recorder.failures, "\n")
}
