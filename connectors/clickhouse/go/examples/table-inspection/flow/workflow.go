// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package tableinspection runs ClickHouse listTables as one Dex Flow.
package tableinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/clickhouse/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ClickHouseTableInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "clickhouse"
	operationStepType = "CallListTables"
)

var tablesAttribute = dex.DefineAttribute[clickhouse.TablesPage]("clickhouse-tables")

// Flow runs ListTables once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection clickhouse.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection clickhouse.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tablesAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(clickhouse.NewListTablesStep(clickhouse.ListTablesStepConfig[clickhouse.ListTablesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ClickHouse", Explanation: "List tables from ClickHouse."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:clickhouse-tables value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTables(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"clickhouse-tables": outcome}}, nil
}

// dex:field attribute-key:clickhouse-tables value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTables(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"clickhouse-tables": outcome}}, nil
}

func readTables(ctx dex.Context) (clickhouse.TablesPage, error) {
	outcome, err := tablesAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return clickhouse.TablesPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input clickhouse.ListTablesInput) clickhouse.ListTablesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListTables" }
func (startRequest) WaitFor(dex.Context, clickhouse.ListTablesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input clickhouse.ListTablesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[clickhouse.ListTablesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[clickhouse.ListTablesResult]
}

func (completeResult) GetStepType() string { return "CompleteListTables" }
func (completeResult) Execute(ctx dex.Context, result clickhouse.ListTablesResult) (*dex.StepDecision, error) {
	if err := tablesAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
