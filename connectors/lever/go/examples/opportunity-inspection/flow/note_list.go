// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package opportunityinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/lever/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// NoteListFlow lists note metadata for one Lever opportunity.
type NoteListFlow struct {
	dex.FlowDefaults
	connection lever.Connection
}

// NewNoteListFlow binds the provider connection at registration time.
func NewNoteListFlow(connection lever.Connection) *NoteListFlow {
	return &NoteListFlow{connection: connection}
}
func (*NoteListFlow) GetFlowType() string { return "LeverNoteList" }

var notesAttribute = dex.DefineAttribute[lever.NotesPage]("lever-notes")

func (*NoteListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{notesAttribute}}
}
func (flow *NoteListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startNoteList{}),
		dex.DefineStep(lever.NewListNotesStep(lever.ListNotesStepConfig[lever.ListNotesInput]{
			StepType: "CallListNotes", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Lever", Explanation: "List notes for the selected opportunity."},
			Connection:  flow.connection, MapToOperationInput: mapNoteListInput,
			Listed: sdkgo.GoTo(completeNoteList{}),
		})),
		dex.DefineStep(completeNoteList{}),
	}
}
func (flow *NoteListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:lever-notes value-type:json editable:false description:"Note metadata"
// GetDexSummary shows the note page in Dex Web.
func (*NoteListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readNotes(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lever-notes": value}}, nil
}

// dex:field attribute-key:lever-notes value-type:json editable:false description:"Note metadata"
// GetDexDisplay shows the note page in Dex Web.
func (*NoteListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readNotes(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lever-notes": value}}, nil
}
func readNotes(ctx dex.Context) (lever.NotesPage, error) {
	value, err := notesAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return lever.NotesPage{}, nil
	}
	return value, err
}
func mapNoteListInput(input lever.ListNotesInput) lever.ListNotesInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the opportunity ID to the provider Step."
type startNoteList struct{ dex.StepDefaults }

func (startNoteList) GetStepType() string { return "StartListNotes" }
func (startNoteList) WaitFor(dex.Context, lever.ListNotesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startNoteList) Execute(_ dex.Context, input lever.ListNotesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[lever.ListNotesInput]("CallListNotes"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the note metadata and complete the Flow."
type completeNoteList struct {
	dex.StepDefaultsNoWaitFor[lever.ListNotesResult]
}

func (completeNoteList) GetStepType() string { return "CompleteListNotes" }
func (completeNoteList) Execute(ctx dex.Context, result lever.ListNotesResult) (*dex.StepDecision, error) {
	if err := notesAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*NoteListFlow)(nil)
