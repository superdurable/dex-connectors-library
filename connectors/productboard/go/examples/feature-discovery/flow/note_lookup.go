// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package featurediscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/productboard/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// NoteLookupFlowType is the stable Flow identity for one note lookup.
const NoteLookupFlowType = "ProductboardNoteLookup"

const noteLookupStepType = "CallGetNote"

var noteAttribute = dex.DefineAttribute[productboard.NoteMetadata]("productboard-note")

// NoteLookupFlow reads note metadata through its operation-specific Step.
type NoteLookupFlow struct {
	dex.FlowDefaults
	connection productboard.Connection
}

// NewNoteLookupFlow binds the provider connection at registration time.
func NewNoteLookupFlow(connection productboard.Connection) *NoteLookupFlow {
	return &NoteLookupFlow{connection: connection}
}

// GetFlowType preserves the stable example Flow identity.
func (*NoteLookupFlow) GetFlowType() string { return NoteLookupFlowType }

// GetPersistenceSchema registers the note metadata Attribute.
func (*NoteLookupFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{noteAttribute}}
}

// GetSteps registers the start, provider lookup, and completion Steps.
func (flow *NoteLookupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startNoteLookup{}),
		dex.DefineStep(productboard.NewGetNoteStep(productboard.GetNoteStepConfig[productboard.GetNoteInput]{
			StepType: noteLookupStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Productboard", Explanation: "Read one Productboard note by ID."},
			Connection:          flow.connection,
			MapToOperationInput: mapNoteLookupInput,
			Found:               sdkgo.GoTo(completeNoteLookup{}),
		})),
		dex.DefineStep(completeNoteLookup{}),
	}
}

// GetRPCs registers read-only Dex Web views.
func (flow *NoteLookupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:productboard-note value-type:json editable:false description:"Note metadata"
// GetDexSummary shows the last note lookup in Dex Web.
func (*NoteLookupFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	note, err := readNote(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"productboard-note": note}}, nil
}

// dex:field attribute-key:productboard-note value-type:json editable:false description:"Note metadata"
// GetDexDisplay shows the last note lookup in Dex Web.
func (*NoteLookupFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	note, err := readNote(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"productboard-note": note}}, nil
}

func readNote(ctx dex.Context) (productboard.NoteMetadata, error) {
	note, err := noteAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return productboard.NoteMetadata{}, nil
	}
	return note, err
}

func mapNoteLookupInput(input productboard.GetNoteInput) productboard.GetNoteInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested note ID to the provider Step."
type startNoteLookup struct{ dex.StepDefaults }

func (startNoteLookup) GetStepType() string { return "StartNoteLookup" }
func (startNoteLookup) WaitFor(dex.Context, productboard.GetNoteInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startNoteLookup) Execute(_ dex.Context, input productboard.GetNoteInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[productboard.GetNoteInput](noteLookupStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one note's metadata."
type completeNoteLookup struct {
	dex.StepDefaultsNoWaitFor[productboard.GetNoteResult]
}

func (completeNoteLookup) GetStepType() string { return "CompleteNoteLookup" }
func (completeNoteLookup) Execute(ctx dex.Context, result productboard.GetNoteResult) (*dex.StepDecision, error) {
	if err := noteAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*NoteLookupFlow)(nil)
