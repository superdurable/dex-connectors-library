// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheetlookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/smartsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// WorkspaceLookupFlowType is the stable identity of the workspace lookup example.
const WorkspaceLookupFlowType = "SmartsheetWorkspaceLookup"

// WorkspaceLookupFlow reads one workspace through the static Smartsheet connection.
type WorkspaceLookupFlow struct {
	dex.FlowDefaults
	connection smartsheet.Connection
}

// NewWorkspaceLookupFlow binds the provider connection at registration.
func NewWorkspaceLookupFlow(connection smartsheet.Connection) *WorkspaceLookupFlow {
	return &WorkspaceLookupFlow{connection: connection}
}

func (*WorkspaceLookupFlow) GetFlowType() string { return WorkspaceLookupFlowType }
func (*WorkspaceLookupFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}
func (flow *WorkspaceLookupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary exposes an empty read-only summary for Dex Web.
func (*WorkspaceLookupFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay exposes an empty read-only display for Dex Web.
func (*WorkspaceLookupFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

func (flow *WorkspaceLookupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startWorkspaceLookup{}),
		dex.DefineStep(smartsheet.NewGetWorkspaceStep(smartsheet.GetWorkspaceStepConfig[smartsheet.GetWorkspaceInput]{
			StepType: "ReadSmartsheetWorkspace", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "smartsheet-workspace", GroupLabel: "Smartsheet workspace", Explanation: "Read one Smartsheet workspace."},
			MapToOperationInput: mapGetWorkspaceInput, Found: sdkgo.GoTo(completeWorkspaceLookup{}),
		})),
		dex.DefineStep(completeWorkspaceLookup{}),
	}
}

func mapGetWorkspaceInput(input smartsheet.GetWorkspaceInput) smartsheet.GetWorkspaceInput {
	return input
}

// dex:group group-id:smartsheet-workspace group-label:"Smartsheet workspace"
// dex:explanation text:"Start the workspace lookup with its sheet and workspace IDs."
type startWorkspaceLookup struct{ dex.StepDefaults }

func (startWorkspaceLookup) GetStepType() string { return "StartSmartsheetWorkspaceLookup" }
func (startWorkspaceLookup) WaitFor(dex.Context, smartsheet.GetWorkspaceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startWorkspaceLookup) Execute(_ dex.Context, input smartsheet.GetWorkspaceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[smartsheet.GetWorkspaceInput]("ReadSmartsheetWorkspace"), input), nil
}

// dex:group group-id:smartsheet-workspace group-label:"Smartsheet workspace"
// dex:explanation text:"Complete with the selected Smartsheet workspace."
type completeWorkspaceLookup struct {
	dex.StepDefaultsNoWaitFor[smartsheet.GetWorkspaceResult]
}

func (completeWorkspaceLookup) GetStepType() string { return "CompleteSmartsheetWorkspaceLookup" }
func (completeWorkspaceLookup) Execute(_ dex.Context, result smartsheet.GetWorkspaceResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*WorkspaceLookupFlow)(nil)
