// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createTagStepType = "CallCreateTag"

var createdTagAttribute = dex.DefineAttribute[activecampaign.TagMetadata]("activecampaign-created-tag")

// CreateTagFlow demonstrates a repeat-safe tag create through a Dex Step.
type CreateTagFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewCreateTagFlow binds the same static connection as the inspection Flow.
func NewCreateTagFlow(connection activecampaign.Connection) *CreateTagFlow {
	return &CreateTagFlow{connection: connection}
}

// GetFlowType is the stable create example identity.
func (*CreateTagFlow) GetFlowType() string { return "ActiveCampaignCreateTag" }

// GetPersistenceSchema registers the created tag displayed in Dex Web.
func (*CreateTagFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdTagAttribute}}
}

// GetSteps registers the start, provider call, and completion boundary.
func (flow *CreateTagFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateTag{}),
		dex.DefineStep(activecampaign.NewCreateTagStep(activecampaign.CreateTagStepConfig[activecampaign.CreateTagInput]{
			StepType: createTagStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Create one contact tag by name."},
			Connection:  flow.connection, MapToOperationInput: MapToCreateTagInput,
			Created: sdkgo.GoTo(completeCreateTag{}),
		})),
		dex.DefineStep(completeCreateTag{}),
	}
}

// GetRPCs exposes the stored tag in Dex Web.
func (flow *CreateTagFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-created-tag value-type:json editable:false description:"Created tag"
// GetDexSummary shows the created tag in Dex Web.
func (*CreateTagFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	tag, err := readCreatedTag(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-tag": tag}}, nil
}

// dex:field attribute-key:activecampaign-created-tag value-type:json editable:false description:"Created tag"
// GetDexDisplay shows the created tag in Dex Web.
func (*CreateTagFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	tag, err := readCreatedTag(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-tag": tag}}, nil
}

func readCreatedTag(ctx dex.Context) (activecampaign.TagMetadata, error) {
	tag, err := createdTagAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.TagMetadata{}, nil
	}
	return tag, err
}

// MapToCreateTagInput preserves the typed start input for the provider call.
func MapToCreateTagInput(input activecampaign.CreateTagInput) activecampaign.CreateTagInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the tag fields to the create Step."
type startCreateTag struct{ dex.StepDefaults }

func (startCreateTag) GetStepType() string { return "StartCreateTag" }
func (startCreateTag) WaitFor(dex.Context, activecampaign.CreateTagInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateTag) Execute(_ dex.Context, input activecampaign.CreateTagInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.CreateTagInput](createTagStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created contact tag metadata."
type completeCreateTag struct {
	dex.StepDefaultsNoWaitFor[activecampaign.CreateTagResult]
}

func (completeCreateTag) GetStepType() string { return "CompleteCreateTag" }
func (completeCreateTag) Execute(ctx dex.Context, result activecampaign.CreateTagResult) (*dex.StepDecision, error) {
	if err := createdTagAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateTagFlow)(nil)
