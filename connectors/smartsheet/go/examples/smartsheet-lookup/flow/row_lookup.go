// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheetlookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/smartsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// RowLookupFlowType is the stable identity of the row lookup example.
const RowLookupFlowType = "SmartsheetRowLookup"

// RowLookupFlow reads one row through the static Smartsheet connection.
type RowLookupFlow struct {
	dex.FlowDefaults
	connection smartsheet.Connection
}

// NewRowLookupFlow binds the provider connection at registration.
func NewRowLookupFlow(connection smartsheet.Connection) *RowLookupFlow {
	return &RowLookupFlow{connection: connection}
}

func (*RowLookupFlow) GetFlowType() string                         { return RowLookupFlowType }
func (*RowLookupFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }
func (flow *RowLookupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary exposes an empty read-only summary for Dex Web.
func (*RowLookupFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay exposes an empty read-only display for Dex Web.
func (*RowLookupFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

func (flow *RowLookupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRowLookup{}),
		dex.DefineStep(smartsheet.NewGetRowStep(smartsheet.GetRowStepConfig[smartsheet.GetRowInput]{
			StepType: "ReadSmartsheetRow", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "smartsheet-row", GroupLabel: "Smartsheet row", Explanation: "Read one Smartsheet row."},
			MapToOperationInput: mapGetRowInput, Found: sdkgo.GoTo(completeRowLookup{}),
		})),
		dex.DefineStep(completeRowLookup{}),
	}
}

func mapGetRowInput(input smartsheet.GetRowInput) smartsheet.GetRowInput { return input }

// dex:group group-id:smartsheet-row group-label:"Smartsheet row"
// dex:explanation text:"Start the row lookup with its sheet and row IDs."
type startRowLookup struct{ dex.StepDefaults }

func (startRowLookup) GetStepType() string { return "StartSmartsheetRowLookup" }
func (startRowLookup) WaitFor(dex.Context, smartsheet.GetRowInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRowLookup) Execute(_ dex.Context, input smartsheet.GetRowInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[smartsheet.GetRowInput]("ReadSmartsheetRow"), input), nil
}

// dex:group group-id:smartsheet-row group-label:"Smartsheet row"
// dex:explanation text:"Complete with the selected Smartsheet row."
type completeRowLookup struct {
	dex.StepDefaultsNoWaitFor[smartsheet.GetRowResult]
}

func (completeRowLookup) GetStepType() string { return "CompleteSmartsheetRowLookup" }
func (completeRowLookup) Execute(_ dex.Context, result smartsheet.GetRowResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*RowLookupFlow)(nil)
