// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package listdatabases runs TDengine Cloud listDatabases as one Dex Flow.
package listdatabases

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/taosdata/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TaosDataListDatabases"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "taosdata"
	operationStepType = "CallListDatabases"
)

var databaseOutcomeAttribute = dex.DefineAttribute[taosdata.ListDatabasesOutput]("taosdata-list-databases-outcome")

// Flow runs ListDatabases once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection taosdata.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection taosdata.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{databaseOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(taosdata.NewListDatabasesStep(taosdata.ListDatabasesStepConfig[taosdata.ListDatabasesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TDengine Cloud", Explanation: "Call the provider's listDatabases operation."},
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

// dex:field attribute-key:taosdata-list-databases-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListDatabasesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"taosdata-list-databases-outcome": outcome}}, nil
}

// dex:field attribute-key:taosdata-list-databases-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListDatabasesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"taosdata-list-databases-outcome": outcome}}, nil
}

func readListDatabasesOutcome(ctx dex.Context) (taosdata.ListDatabasesOutput, error) {
	outcome, err := databaseOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return taosdata.ListDatabasesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input taosdata.ListDatabasesInput) taosdata.ListDatabasesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListDatabases" }
func (startRequest) WaitFor(dex.Context, taosdata.ListDatabasesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input taosdata.ListDatabasesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[taosdata.ListDatabasesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[taosdata.ListDatabasesResult]
}

func (completeResult) GetStepType() string { return "CompleteListDatabases" }
func (completeResult) Execute(ctx dex.Context, result taosdata.ListDatabasesResult) (*dex.StepDecision, error) {
	if err := databaseOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
