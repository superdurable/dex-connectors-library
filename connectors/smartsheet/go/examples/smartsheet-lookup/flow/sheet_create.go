// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheetlookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/smartsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SheetCreateFlowType is the stable identity of the sheet create example.
const SheetCreateFlowType = "SmartsheetSheetCreate"

// SheetCreateFlow creates one sheet in a workspace.
type SheetCreateFlow struct {
	dex.FlowDefaults
	connection smartsheet.Connection
}

// NewSheetCreateFlow binds the Smartsheet connection.
func NewSheetCreateFlow(connection smartsheet.Connection) *SheetCreateFlow {
	return &SheetCreateFlow{connection: connection}
}

func (*SheetCreateFlow) GetFlowType() string                         { return SheetCreateFlowType }
func (*SheetCreateFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }
func (flow *SheetCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary exposes an empty read-only summary.
func (*SheetCreateFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay exposes an empty read-only display.
func (*SheetCreateFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

func (flow *SheetCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSheetCreate{}),
		dex.DefineStep(smartsheet.NewCreateSheetInWorkspaceStep(smartsheet.CreateSheetInWorkspaceStepConfig[smartsheet.CreateSheetInWorkspaceInput]{
			StepType: "CreateSmartsheetSheet", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "smartsheet-sheet", GroupLabel: "Smartsheet sheet", Explanation: "Create one sheet in a workspace."},
			MapToOperationInput: mapCreateSheetInput, Created: sdkgo.GoTo(completeSheetCreate{}),
		})),
		dex.DefineStep(completeSheetCreate{}),
	}
}

func mapCreateSheetInput(input smartsheet.CreateSheetInWorkspaceInput) smartsheet.CreateSheetInWorkspaceInput {
	return input
}

// dex:group group-id:smartsheet-sheet group-label:"Smartsheet sheet"
// dex:explanation text:"Start sheet creation with a workspace ID, name, and primary column title."
type startSheetCreate struct{ dex.StepDefaults }

func (startSheetCreate) GetStepType() string { return "StartSmartsheetSheetCreate" }
func (startSheetCreate) WaitFor(dex.Context, smartsheet.CreateSheetInWorkspaceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSheetCreate) Execute(_ dex.Context, input smartsheet.CreateSheetInWorkspaceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[smartsheet.CreateSheetInWorkspaceInput]("CreateSmartsheetSheet"), input), nil
}

// dex:group group-id:smartsheet-sheet group-label:"Smartsheet sheet"
// dex:explanation text:"Complete with the created sheet identity."
type completeSheetCreate struct {
	dex.StepDefaultsNoWaitFor[smartsheet.CreateSheetInWorkspaceResult]
}

func (completeSheetCreate) GetStepType() string { return "CompleteSmartsheetSheetCreate" }
func (completeSheetCreate) Execute(_ dex.Context, result smartsheet.CreateSheetInWorkspaceResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SheetCreateFlow)(nil)
