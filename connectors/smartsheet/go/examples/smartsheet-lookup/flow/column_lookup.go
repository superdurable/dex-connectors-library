// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheetlookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/smartsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ColumnLookupFlowType is the stable identity of the column lookup example.
const ColumnLookupFlowType = "SmartsheetColumnLookup"

// ColumnLookupFlow reads one column through the static Smartsheet connection.
type ColumnLookupFlow struct {
	dex.FlowDefaults
	connection smartsheet.Connection
}

// NewColumnLookupFlow binds the provider connection at registration.
func NewColumnLookupFlow(connection smartsheet.Connection) *ColumnLookupFlow {
	return &ColumnLookupFlow{connection: connection}
}

func (*ColumnLookupFlow) GetFlowType() string                         { return ColumnLookupFlowType }
func (*ColumnLookupFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }
func (flow *ColumnLookupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary exposes an empty read-only summary for Dex Web.
func (*ColumnLookupFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay exposes an empty read-only display for Dex Web.
func (*ColumnLookupFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

func (flow *ColumnLookupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startColumnLookup{}),
		dex.DefineStep(smartsheet.NewGetColumnStep(smartsheet.GetColumnStepConfig[smartsheet.GetColumnInput]{
			StepType: "ReadSmartsheetColumn", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "smartsheet-column", GroupLabel: "Smartsheet column", Explanation: "Read one Smartsheet column."},
			MapToOperationInput: mapGetColumnInput, Found: sdkgo.GoTo(completeColumnLookup{}),
		})),
		dex.DefineStep(completeColumnLookup{}),
	}
}

func mapGetColumnInput(input smartsheet.GetColumnInput) smartsheet.GetColumnInput { return input }

// dex:group group-id:smartsheet-column group-label:"Smartsheet column"
// dex:explanation text:"Start the column lookup with its sheet and column IDs."
type startColumnLookup struct{ dex.StepDefaults }

func (startColumnLookup) GetStepType() string { return "StartSmartsheetColumnLookup" }
func (startColumnLookup) WaitFor(dex.Context, smartsheet.GetColumnInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startColumnLookup) Execute(_ dex.Context, input smartsheet.GetColumnInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[smartsheet.GetColumnInput]("ReadSmartsheetColumn"), input), nil
}

// dex:group group-id:smartsheet-column group-label:"Smartsheet column"
// dex:explanation text:"Complete with the selected Smartsheet column."
type completeColumnLookup struct {
	dex.StepDefaultsNoWaitFor[smartsheet.GetColumnResult]
}

func (completeColumnLookup) GetStepType() string { return "CompleteSmartsheetColumnLookup" }
func (completeColumnLookup) Execute(_ dex.Context, result smartsheet.GetColumnResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ColumnLookupFlow)(nil)
