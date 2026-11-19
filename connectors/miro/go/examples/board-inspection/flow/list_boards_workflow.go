// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package boardinspection

import (
	"github.com/superdurable/dex-connectors-library/connectors/miro/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ListBoardsFlowType is the stable identity for board discovery.
const ListBoardsFlowType = "MiroBoardDiscovery"

// ListBoardsFlow reads one page of boards accessible to the connection.
type ListBoardsFlow struct {
	dex.FlowDefaults
	connection miro.Connection
}

// NewListBoardsFlow binds the Miro connection at registration.
func NewListBoardsFlow(connection miro.Connection) *ListBoardsFlow {
	return &ListBoardsFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*ListBoardsFlow) GetFlowType() string { return ListBoardsFlowType }

// GetPersistenceSchema declares no persistent Attributes or Streams.
func (*ListBoardsFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// GetRPCs exposes the Flow status in Dex Web.
func (flow *ListBoardsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary describes where to find the completed board page.
func (*ListBoardsFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay describes where to find the completed board page.
func (*ListBoardsFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetSteps declares the start, provider, and completion Steps.
func (flow *ListBoardsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startListBoards{}),
		dex.DefineStep(miro.NewListBoardsStep(miro.ListBoardsStepConfig[miro.ListBoardsInput]{StepType: "CallListBoards", ConnectionName: ConnectionName, Connection: flow.connection, MapToOperationInput: mapListBoardsInput, Listed: sdkgo.GoTo(completeListBoards{}), Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Miro", Explanation: "List accessible Miro boards."}})),
		dex.DefineStep(completeListBoards{}),
	}
}

func mapListBoardsInput(input miro.ListBoardsInput) miro.ListBoardsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start board discovery."
type startListBoards struct{ dex.StepDefaults }

func (startListBoards) GetStepType() string { return "StartListBoards" }
func (startListBoards) WaitFor(dex.Context, miro.ListBoardsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startListBoards) Execute(_ dex.Context, input miro.ListBoardsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[miro.ListBoardsInput]("CallListBoards"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the board page."
type completeListBoards struct {
	dex.StepDefaultsNoWaitFor[miro.ListBoardsResult]
}

func (completeListBoards) GetStepType() string { return "CompleteListBoards" }
func (completeListBoards) Execute(_ dex.Context, result miro.ListBoardsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ListBoardsFlow)(nil)
