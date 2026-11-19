// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workerinspection

import (
	"github.com/superdurable/dex-connectors-library/connectors/rippling/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const rosterOperationStepType = "CallListWorkers"

var rosterOperationAttribute = dex.DefineAttribute[string]("operation")

// RosterFlow lists one worker page with the example connection.
type RosterFlow struct {
	dex.FlowDefaults
	connection rippling.Connection
}

// NewRosterFlow binds the provider connection at registration time.
func NewRosterFlow(connection rippling.Connection) *RosterFlow {
	return &RosterFlow{connection: connection}
}

// GetPersistenceSchema retains the operation label for Dex Web views.
func (*RosterFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{rosterOperationAttribute}}
}

// GetSteps registers the worker list Query and result Step.
func (flow *RosterFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRoster{}),
		dex.DefineStep(rippling.NewListWorkersStep(rippling.ListWorkersStepConfig[rippling.ListWorkersInput]{
			StepType:            rosterOperationStepType,
			ConnectionName:      ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Rippling", Explanation: "List visible Rippling workers."},
			Connection:          flow.connection,
			MapToOperationInput: mapRosterInput,
			Found:               sdkgo.GoTo(completeRoster{}),
		})),
		dex.DefineStep(completeRoster{}),
	}
}

// GetRPCs exposes the roster Flow's read-only Dex Web views.
func (flow *RosterFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:operation value-type:string editable:false description:"Provider operation"
// GetDexSummary describes the roster Flow in Dex Web.
func (*RosterFlow) GetDexSummary(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"operation": "listWorkers"}}, nil
}

// dex:field attribute-key:operation value-type:string editable:false description:"Provider operation"
// GetDexDisplay describes the roster Flow in Dex Web.
func (*RosterFlow) GetDexDisplay(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"operation": "listWorkers"}}, nil
}

func mapRosterInput(input rippling.ListWorkersInput) rippling.ListWorkersInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the page request to the provider Step."
type startRoster struct{ dex.StepDefaults }

func (startRoster) WaitFor(dex.Context, rippling.ListWorkersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (startRoster) Execute(ctx dex.Context, input rippling.ListWorkersInput) (*dex.StepDecision, error) {
	if err := rosterOperationAttribute.Set(ctx, "listWorkers"); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[rippling.ListWorkersInput](rosterOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the worker page."
type completeRoster struct {
	dex.StepDefaultsNoWaitFor[rippling.ListWorkersResult]
}

func (completeRoster) Execute(_ dex.Context, result rippling.ListWorkersResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}
