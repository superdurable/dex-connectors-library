// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const subscribeContactToListStepType = "CallSubscribeContactToList"

var subscribedContactAttribute = dex.DefineAttribute[activecampaign.ContactListMembership]("activecampaign-subscribed-contact-list")

// SubscribeContactToListFlow demonstrates bounded subscription with read-back.
type SubscribeContactToListFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewSubscribeContactToListFlow binds the same static connection as the inspection Flow.
func NewSubscribeContactToListFlow(connection activecampaign.Connection) *SubscribeContactToListFlow {
	return &SubscribeContactToListFlow{connection: connection}
}

// GetFlowType is the stable subscription example identity.
func (*SubscribeContactToListFlow) GetFlowType() string {
	return "ActiveCampaignSubscribeContactToList"
}

// GetPersistenceSchema registers the subscription displayed in Dex Web.
func (*SubscribeContactToListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{subscribedContactAttribute}}
}

// GetSteps registers the start, provider call, and completion boundary.
func (flow *SubscribeContactToListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSubscribeContactToList{}),
		dex.DefineStep(activecampaign.NewSubscribeContactToListStep(activecampaign.SubscribeContactToListStepConfig[activecampaign.SetContactListMembershipInput]{
			StepType: subscribeContactToListStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Subscribe one contact to an email list by ID."},
			Connection:  flow.connection, MapToOperationInput: MapToSubscribeContactToListInput,
			Subscribed: sdkgo.GoTo(completeSubscribeContactToList{}),
		})),
		dex.DefineStep(completeSubscribeContactToList{}),
	}
}

// GetRPCs exposes the stored subscription in Dex Web.
func (flow *SubscribeContactToListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-subscribed-contact-list value-type:json editable:false description:"List subscription"
// GetDexSummary shows the subscription in Dex Web.
func (*SubscribeContactToListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	membership, err := readSubscribedMembership(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-subscribed-contact-list": membership}}, nil
}

// dex:field attribute-key:activecampaign-subscribed-contact-list value-type:json editable:false description:"List subscription"
// GetDexDisplay shows the subscription in Dex Web.
func (*SubscribeContactToListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	membership, err := readSubscribedMembership(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-subscribed-contact-list": membership}}, nil
}

func readSubscribedMembership(ctx dex.Context) (activecampaign.ContactListMembership, error) {
	membership, err := subscribedContactAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.ContactListMembership{}, nil
	}
	return membership, err
}

// MapToSubscribeContactToListInput preserves the typed start input for the provider call.
func MapToSubscribeContactToListInput(input activecampaign.SetContactListMembershipInput) activecampaign.SetContactListMembershipInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass contact and list IDs to the subscribe Step."
type startSubscribeContactToList struct{ dex.StepDefaults }

func (startSubscribeContactToList) GetStepType() string { return "StartSubscribeContactToList" }
func (startSubscribeContactToList) WaitFor(dex.Context, activecampaign.SetContactListMembershipInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSubscribeContactToList) Execute(_ dex.Context, input activecampaign.SetContactListMembershipInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.SetContactListMembershipInput](subscribeContactToListStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the contact-list membership."
type completeSubscribeContactToList struct {
	dex.StepDefaultsNoWaitFor[activecampaign.SubscribeContactToListResult]
}

func (completeSubscribeContactToList) GetStepType() string { return "CompleteSubscribeContactToList" }
func (completeSubscribeContactToList) Execute(ctx dex.Context, result activecampaign.SubscribeContactToListResult) (*dex.StepDecision, error) {
	if err := subscribedContactAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SubscribeContactToListFlow)(nil)
