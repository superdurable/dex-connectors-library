// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheetlookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/smartsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FolderLookupFlowType is the stable identity of the folder lookup example.
const FolderLookupFlowType = "SmartsheetFolderLookup"

// FolderLookupFlow reads one folder through the static Smartsheet connection.
type FolderLookupFlow struct {
	dex.FlowDefaults
	connection smartsheet.Connection
}

// NewFolderLookupFlow binds the provider connection at registration.
func NewFolderLookupFlow(connection smartsheet.Connection) *FolderLookupFlow {
	return &FolderLookupFlow{connection: connection}
}

func (*FolderLookupFlow) GetFlowType() string                         { return FolderLookupFlowType }
func (*FolderLookupFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }
func (flow *FolderLookupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary exposes an empty read-only summary for Dex Web.
func (*FolderLookupFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay exposes an empty read-only display for Dex Web.
func (*FolderLookupFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

func (flow *FolderLookupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startFolderLookup{}),
		dex.DefineStep(smartsheet.NewGetFolderStep(smartsheet.GetFolderStepConfig[smartsheet.GetFolderInput]{
			StepType: "ReadSmartsheetFolder", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "smartsheet-folder", GroupLabel: "Smartsheet folder", Explanation: "Read one Smartsheet folder."},
			MapToOperationInput: mapGetFolderInput, Found: sdkgo.GoTo(completeFolderLookup{}),
		})),
		dex.DefineStep(completeFolderLookup{}),
	}
}

func mapGetFolderInput(input smartsheet.GetFolderInput) smartsheet.GetFolderInput { return input }

// dex:group group-id:smartsheet-folder group-label:"Smartsheet folder"
// dex:explanation text:"Start the folder lookup with its sheet and folder IDs."
type startFolderLookup struct{ dex.StepDefaults }

func (startFolderLookup) GetStepType() string { return "StartSmartsheetFolderLookup" }
func (startFolderLookup) WaitFor(dex.Context, smartsheet.GetFolderInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startFolderLookup) Execute(_ dex.Context, input smartsheet.GetFolderInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[smartsheet.GetFolderInput]("ReadSmartsheetFolder"), input), nil
}

// dex:group group-id:smartsheet-folder group-label:"Smartsheet folder"
// dex:explanation text:"Complete with the selected Smartsheet folder."
type completeFolderLookup struct {
	dex.StepDefaultsNoWaitFor[smartsheet.GetFolderResult]
}

func (completeFolderLookup) GetStepType() string { return "CompleteSmartsheetFolderLookup" }
func (completeFolderLookup) Execute(_ dex.Context, result smartsheet.GetFolderResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*FolderLookupFlow)(nil)
