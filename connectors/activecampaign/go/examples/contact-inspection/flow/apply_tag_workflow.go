// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const applyTagToContactStepType = "CallApplyTagToContact"

var appliedTagAttribute = dex.DefineAttribute[activecampaign.ContactTagAssociation]("activecampaign-applied-tag")

// ApplyTagToContactFlow demonstrates repeat-safe assignment of an existing tag.
type ApplyTagToContactFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewApplyTagToContactFlow binds the same static connection as the inspection Flow.
func NewApplyTagToContactFlow(connection activecampaign.Connection) *ApplyTagToContactFlow {
	return &ApplyTagToContactFlow{connection: connection}
}

// GetFlowType is the stable tag-assignment example identity.
func (*ApplyTagToContactFlow) GetFlowType() string { return "ActiveCampaignApplyTagToContact" }

// GetPersistenceSchema registers the applied tag displayed in Dex Web.
func (*ApplyTagToContactFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{appliedTagAttribute}}
}

// GetSteps registers the start, provider call, and completion boundary.
func (flow *ApplyTagToContactFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startApplyTagToContact{}),
		dex.DefineStep(activecampaign.NewApplyTagToContactStep(activecampaign.ApplyTagToContactStepConfig[activecampaign.ApplyTagToContactInput]{
			StepType: applyTagToContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Apply an existing tag to one contact by ID."},
			Connection:  flow.connection, MapToOperationInput: MapToApplyTagToContactInput,
			Applied: sdkgo.GoTo(completeApplyTagToContact{}),
		})),
		dex.DefineStep(completeApplyTagToContact{}),
	}
}

// GetRPCs exposes the stored tag in Dex Web.
func (flow *ApplyTagToContactFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-applied-tag value-type:json editable:false description:"Applied tag"
// GetDexSummary shows the applied tag in Dex Web.
func (*ApplyTagToContactFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	tag, err := readAppliedTag(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-applied-tag": tag}}, nil
}

// dex:field attribute-key:activecampaign-applied-tag value-type:json editable:false description:"Applied tag"
// GetDexDisplay shows the applied tag in Dex Web.
func (*ApplyTagToContactFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	tag, err := readAppliedTag(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-applied-tag": tag}}, nil
}

func readAppliedTag(ctx dex.Context) (activecampaign.ContactTagAssociation, error) {
	tag, err := appliedTagAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.ContactTagAssociation{}, nil
	}
	return tag, err
}

// MapToApplyTagToContactInput preserves the typed start input for the provider call.
func MapToApplyTagToContactInput(input activecampaign.ApplyTagToContactInput) activecampaign.ApplyTagToContactInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass contact and existing tag IDs to the provider Step."
type startApplyTagToContact struct{ dex.StepDefaults }

func (startApplyTagToContact) GetStepType() string { return "StartApplyTagToContact" }
func (startApplyTagToContact) WaitFor(dex.Context, activecampaign.ApplyTagToContactInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startApplyTagToContact) Execute(_ dex.Context, input activecampaign.ApplyTagToContactInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.ApplyTagToContactInput](applyTagToContactStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the applied contact tag metadata."
type completeApplyTagToContact struct {
	dex.StepDefaultsNoWaitFor[activecampaign.ApplyTagToContactResult]
}

func (completeApplyTagToContact) GetStepType() string { return "CompleteApplyTagToContact" }
func (completeApplyTagToContact) Execute(ctx dex.Context, result activecampaign.ApplyTagToContactResult) (*dex.StepDecision, error) {
	if err := appliedTagAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ApplyTagToContactFlow)(nil)
