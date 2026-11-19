// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workerinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/workday/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// SearchFlowType is the stable worker search Flow identity.
	SearchFlowType = "WorkdayWorkerSearch"
	searchStepType = "CallListWorkers"
)

var workerPageAttribute = dex.DefineAttribute[workday.WorkerPage]("workday-worker-page")

// SearchFlow runs a paginated worker search.
type SearchFlow struct {
	dex.FlowDefaults
	connection workday.Connection
}

// NewSearchFlow binds the provider connection at registration time.
func NewSearchFlow(connection workday.Connection) *SearchFlow {
	return &SearchFlow{connection: connection}
}

func (*SearchFlow) GetFlowType() string { return SearchFlowType }

func (*SearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{workerPageAttribute}}
}

func (flow *SearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSearch{}),
		dex.DefineStep(workday.NewListWorkersStep(workday.ListWorkersStepConfig[workday.ListWorkersInput]{
			StepType: searchStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Workday", Explanation: "Search accessible workers by name and page."},
			Connection:  flow.connection, MapToOperationInput: mapSearchInput,
			Found: sdkgo.GoTo(completeSearch{}),
		})),
		dex.DefineStep(completeSearch{}),
	}
}

func (flow *SearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:workday-worker-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last worker page in Dex Web.
func (*SearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readWorkerPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"workday-worker-page": page}}, nil
}

// dex:field attribute-key:workday-worker-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last worker page in Dex Web.
func (*SearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readWorkerPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"workday-worker-page": page}}, nil
}

func readWorkerPage(ctx dex.Context) (workday.WorkerPage, error) {
	page, err := workerPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return workday.WorkerPage{}, nil
	}
	return page, err
}

func mapSearchInput(input workday.ListWorkersInput) workday.ListWorkersInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass page options to the Workday Query."
type startSearch struct{ dex.StepDefaults }

func (startSearch) GetStepType() string { return "StartListWorkers" }
func (startSearch) WaitFor(dex.Context, workday.ListWorkersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSearch) Execute(_ dex.Context, input workday.ListWorkersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[workday.ListWorkersInput](searchStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the typed worker page."
type completeSearch struct {
	dex.StepDefaultsNoWaitFor[workday.ListWorkersResult]
}

func (completeSearch) GetStepType() string { return "CompleteListWorkers" }
func (completeSearch) Execute(ctx dex.Context, result workday.ListWorkersResult) (*dex.StepDecision, error) {
	if err := workerPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SearchFlow)(nil)
