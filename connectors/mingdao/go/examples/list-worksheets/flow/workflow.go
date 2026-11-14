// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package worksheets runs Mingdao listWorksheets as one Dex Flow.
package worksheets

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/mingdao/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MingdaoWorksheets"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "mingdao"
	operationStepType = "CallListWorksheets"
)

var worksheetOutcomeAttribute = dex.DefineAttribute[mingdao.ListWorksheetsOutput]("mingdao-list-worksheets-outcome")

// Flow runs ListWorksheets once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection mingdao.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection mingdao.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{worksheetOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(mingdao.NewListWorksheetsStep(mingdao.ListWorksheetsStepConfig[mingdao.ListWorksheetsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Mingdao", Explanation: "Call the provider's listWorksheets operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:mingdao-list-worksheets-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListWorksheetsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"mingdao-list-worksheets-outcome": outcome}}, nil
}

// dex:field attribute-key:mingdao-list-worksheets-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListWorksheetsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"mingdao-list-worksheets-outcome": outcome}}, nil
}

func readListWorksheetsOutcome(ctx dex.Context) (mingdao.ListWorksheetsOutput, error) {
	outcome, err := worksheetOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return mingdao.ListWorksheetsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input mingdao.ListWorksheetsInput) mingdao.ListWorksheetsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListWorksheets" }
func (startRequest) WaitFor(dex.Context, mingdao.ListWorksheetsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input mingdao.ListWorksheetsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[mingdao.ListWorksheetsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[mingdao.ListWorksheetsResult]
}

func (completeResult) GetStepType() string { return "CompleteListWorksheets" }
func (completeResult) Execute(ctx dex.Context, result mingdao.ListWorksheetsResult) (*dex.StepDecision, error) {
	if err := worksheetOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
