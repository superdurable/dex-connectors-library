// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createDealNoteStepType = "CallCreateDealNote"

var createdDealNoteAttribute = dex.DefineAttribute[activecampaign.DealNoteMetadata]("activecampaign-created-deal-note")

// CreateDealNoteFlow demonstrates a bounded deal note create through a Dex Step.
type CreateDealNoteFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewCreateDealNoteFlow binds the same static connection as the inspection Flow.
func NewCreateDealNoteFlow(connection activecampaign.Connection) *CreateDealNoteFlow {
	return &CreateDealNoteFlow{connection: connection}
}

// GetFlowType is the stable create example identity.
func (*CreateDealNoteFlow) GetFlowType() string { return "ActiveCampaignCreateDealNote" }

// GetPersistenceSchema registers the created deal displayed in Dex Web.
func (*CreateDealNoteFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdDealNoteAttribute}}
}

// GetSteps registers the start, provider call, and completion boundary.
func (flow *CreateDealNoteFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateDealNote{}),
		dex.DefineStep(activecampaign.NewCreateDealNoteStep(activecampaign.CreateDealNoteStepConfig[activecampaign.CreateDealNoteInput]{
			StepType: createDealNoteStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Create one note on an existing deal."},
			Connection:  flow.connection, MapToOperationInput: MapToCreateDealNoteInput,
			Created: sdkgo.GoTo(completeCreateDealNote{}),
		})),
		dex.DefineStep(completeCreateDealNote{}),
	}
}

// GetRPCs exposes the stored deal in Dex Web.
func (flow *CreateDealNoteFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-created-deal-note value-type:json editable:false description:"Created deal note"
// GetDexSummary shows the created deal note in Dex Web.
func (*CreateDealNoteFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	note, err := readCreatedDealNote(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-deal-note": note}}, nil
}

// dex:field attribute-key:activecampaign-created-deal-note value-type:json editable:false description:"Created deal note"
// GetDexDisplay shows the created deal note in Dex Web.
func (*CreateDealNoteFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	note, err := readCreatedDealNote(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-deal-note": note}}, nil
}

func readCreatedDealNote(ctx dex.Context) (activecampaign.DealNoteMetadata, error) {
	note, err := createdDealNoteAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.DealNoteMetadata{}, nil
	}
	return note, err
}

// MapToCreateDealNoteInput preserves the typed start input for the provider call.
func MapToCreateDealNoteInput(input activecampaign.CreateDealNoteInput) activecampaign.CreateDealNoteInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the deal fields to the create Step."
type startCreateDealNote struct{ dex.StepDefaults }

func (startCreateDealNote) GetStepType() string { return "StartCreateDealNote" }
func (startCreateDealNote) WaitFor(dex.Context, activecampaign.CreateDealNoteInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateDealNote) Execute(_ dex.Context, input activecampaign.CreateDealNoteInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.CreateDealNoteInput](createDealNoteStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created contact deal metadata."
type completeCreateDealNote struct {
	dex.StepDefaultsNoWaitFor[activecampaign.CreateDealNoteResult]
}

func (completeCreateDealNote) GetStepType() string { return "CompleteCreateDealNote" }
func (completeCreateDealNote) Execute(ctx dex.Context, result activecampaign.CreateDealNoteResult) (*dex.StepDecision, error) {
	if err := createdDealNoteAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateDealNoteFlow)(nil)
