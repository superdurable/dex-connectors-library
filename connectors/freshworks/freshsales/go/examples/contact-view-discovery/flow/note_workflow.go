// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactviewdiscovery

import (
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshsales/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const noteCreationStepType = "CallCreateNote"

var noteIDAttribute = dex.DefineAttribute[int64]("freshsales-created-note-id")

// NoteFlow creates a note only when explicitly started with a target and text.
type NoteFlow struct {
	dex.FlowDefaults
	connection freshsales.Connection
}

// NewNoteFlow binds the provider connection at registration time.
func NewNoteFlow(connection freshsales.Connection) *NoteFlow {
	return &NoteFlow{connection: connection}
}

// GetFlowType returns the stable example Flow identity.
func (*NoteFlow) GetFlowType() string { return "FreshsalesNoteCreation" }

// GetPersistenceSchema registers the created note ID for Dex Web views.
func (*NoteFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{noteIDAttribute}}
}

// GetRPCs exposes read-only summary and display views.
func (flow *NoteFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:freshsales-created-note-id value-type:int64 editable:false description:"Created note ID"
// GetDexSummary shows the created note ID in Dex Web.
func (*NoteFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := noteIDAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"freshsales-created-note-id": value}}, nil
}

// dex:field attribute-key:freshsales-created-note-id value-type:int64 editable:false description:"Created note ID"
// GetDexDisplay shows the created note ID in Dex Web.
func (*NoteFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := noteIDAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"freshsales-created-note-id": value}}, nil
}

// GetSteps defines the explicit note creation path.
func (flow *NoteFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startNoteCreation{}),
		dex.DefineStep(freshsales.NewCreateNoteStep(freshsales.CreateNoteStepConfig[freshsales.CreateNoteInput]{
			StepType: noteCreationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Freshsales", Explanation: "Create the requested note on a CRM record."},
			Connection:  flow.connection, MapToOperationInput: mapNoteCreationInput,
			Created: sdkgo.GoTo(completeNoteCreation{}),
		})),
		dex.DefineStep(completeNoteCreation{}),
	}
}

func mapNoteCreationInput(input freshsales.CreateNoteInput) freshsales.CreateNoteInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start an explicitly requested note creation."
type startNoteCreation struct{ dex.StepDefaults }

func (startNoteCreation) GetStepType() string { return "StartCreateNote" }
func (startNoteCreation) WaitFor(dex.Context, freshsales.CreateNoteInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startNoteCreation) Execute(_ dex.Context, input freshsales.CreateNoteInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[freshsales.CreateNoteInput](noteCreationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after Freshsales confirms the new note ID."
type completeNoteCreation struct {
	dex.StepDefaultsNoWaitFor[freshsales.CreateNoteResult]
}

func (completeNoteCreation) GetStepType() string { return "CompleteCreateNote" }
func (completeNoteCreation) Execute(ctx dex.Context, result freshsales.CreateNoteResult) (*dex.StepDecision, error) {
	if err := noteIDAttribute.Set(ctx, result.Value.ID); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*NoteFlow)(nil)
