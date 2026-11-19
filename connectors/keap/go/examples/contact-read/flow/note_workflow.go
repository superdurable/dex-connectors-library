// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/keap/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// NoteFlowType identifies the contact note lookup example Flow.
const NoteFlowType = "KeapContactNoteLookup"

const getContactNoteStepType = "CallGetContactNote"

var contactNoteAttribute = dex.DefineAttribute[keap.ContactNote]("keap-contact-note")

// NoteFlow reads one note from a Keap contact.
type NoteFlow struct {
	dex.FlowDefaults
	connection keap.Connection
}

// NewNoteFlow binds the static Keap connection at registration.
func NewNoteFlow(connection keap.Connection) *NoteFlow { return &NoteFlow{connection: connection} }

func (*NoteFlow) GetFlowType() string { return NoteFlowType }

func (*NoteFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactNoteAttribute}}
}

func (flow *NoteFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startNoteLookup{}),
		dex.DefineStep(keap.NewGetContactNoteStep(keap.GetContactNoteStepConfig[keap.ContactNoteIDInput]{
			StepType: getContactNoteStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Keap", Explanation: "Read one CRM contact note."},
			Connection:  flow.connection, MapToOperationInput: mapContactNoteID,
			Found: sdkgo.GoTo(completeNoteLookup{}),
		})),
		dex.DefineStep(completeNoteLookup{}),
	}
}

func (flow *NoteFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:keap-contact-note value-type:json editable:false description:"Contact note"
// GetDexSummary shows the contact note in Dex Web.
func (*NoteFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	note, err := readContactNote(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-contact-note": note}}, nil
}

// dex:field attribute-key:keap-contact-note value-type:json editable:false description:"Contact note"
// GetDexDisplay shows the contact note in Dex Web.
func (*NoteFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	note, err := readContactNote(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-contact-note": note}}, nil
}

func readContactNote(ctx dex.Context) (keap.ContactNote, error) {
	note, err := contactNoteAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return keap.ContactNote{}, nil
	}
	return note, err
}

func mapContactNoteID(input keap.ContactNoteIDInput) keap.ContactNoteIDInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected contact and note IDs to the provider Step."
type startNoteLookup struct{ dex.StepDefaults }

func (startNoteLookup) GetStepType() string { return "StartContactNoteLookup" }
func (startNoteLookup) WaitFor(dex.Context, keap.ContactNoteIDInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startNoteLookup) Execute(_ dex.Context, input keap.ContactNoteIDInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[keap.ContactNoteIDInput](getContactNoteStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the returned note and complete the Flow."
type completeNoteLookup struct {
	dex.StepDefaultsNoWaitFor[keap.GetContactNoteResult]
}

func (completeNoteLookup) GetStepType() string { return "CompleteContactNoteLookup" }
func (completeNoteLookup) Execute(ctx dex.Context, result keap.GetContactNoteResult) (*dex.StepDecision, error) {
	if err := contactNoteAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*NoteFlow)(nil)
