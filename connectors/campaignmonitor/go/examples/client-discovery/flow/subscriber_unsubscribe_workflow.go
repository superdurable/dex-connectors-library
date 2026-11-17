// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clientdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/campaignmonitor/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SubscriberUnsubscribeFlowType is the stable example unsubscribe identity.
const SubscriberUnsubscribeFlowType = "CampaignMonitorSubscriberUnsubscribe"

const unsubscribeSubscriberStepType = "CallUnsubscribeSubscriber"

var unsubscribedSubscriberAttribute = dex.DefineAttribute[campaignmonitor.SubscriberIdentity]("campaignmonitor-unsubscribed-subscriber")

// SubscriberUnsubscribeFlow unsubscribes one address from a list.
type SubscriberUnsubscribeFlow struct {
	dex.FlowDefaults
	connection campaignmonitor.Connection
}

// NewSubscriberUnsubscribeFlow binds the provider connection at registration time.
func NewSubscriberUnsubscribeFlow(connection campaignmonitor.Connection) *SubscriberUnsubscribeFlow {
	return &SubscriberUnsubscribeFlow{connection: connection}
}

func (*SubscriberUnsubscribeFlow) GetFlowType() string { return SubscriberUnsubscribeFlowType }

func (*SubscriberUnsubscribeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{unsubscribedSubscriberAttribute}}
}

func (flow *SubscriberUnsubscribeFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:campaignmonitor-unsubscribed-subscriber value-type:json editable:false description:"Unsubscribed subscriber"
// GetDexSummary shows the selected subscriber in Dex Web.
func (*SubscriberUnsubscribeFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	identity, err := readUnsubscribedSubscriber(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"campaignmonitor-unsubscribed-subscriber": identity}}, nil
}

// dex:field attribute-key:campaignmonitor-unsubscribed-subscriber value-type:json editable:false description:"Unsubscribed subscriber"
// GetDexDisplay shows the selected subscriber in Dex Web.
func (*SubscriberUnsubscribeFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	identity, err := readUnsubscribedSubscriber(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"campaignmonitor-unsubscribed-subscriber": identity}}, nil
}

func readUnsubscribedSubscriber(ctx dex.Context) (campaignmonitor.SubscriberIdentity, error) {
	identity, err := unsubscribedSubscriberAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return campaignmonitor.SubscriberIdentity{}, nil
	}
	return identity, err
}

func (flow *SubscriberUnsubscribeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSubscriberUnsubscribe{}),
		dex.DefineStep(campaignmonitor.NewUnsubscribeSubscriberStep(campaignmonitor.UnsubscribeSubscriberStepConfig[campaignmonitor.UnsubscribeSubscriberInput]{
			StepType: unsubscribeSubscriberStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Campaign Monitor", Explanation: "Unsubscribe the selected email from one subscriber list."},
			Connection:          flow.connection,
			MapToOperationInput: mapUnsubscribeSubscriberInput,
			Unsubscribed:        sdkgo.GoTo(completeSubscriberUnsubscribe{}),
		})),
		dex.DefineStep(completeSubscriberUnsubscribe{}),
	}
}

func mapUnsubscribeSubscriberInput(input campaignmonitor.UnsubscribeSubscriberInput) campaignmonitor.UnsubscribeSubscriberInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected list and email to the provider Step."
type startSubscriberUnsubscribe struct{ dex.StepDefaults }

func (startSubscriberUnsubscribe) GetStepType() string { return "StartUnsubscribeSubscriber" }
func (startSubscriberUnsubscribe) WaitFor(dex.Context, campaignmonitor.UnsubscribeSubscriberInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSubscriberUnsubscribe) Execute(_ dex.Context, input campaignmonitor.UnsubscribeSubscriberInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[campaignmonitor.UnsubscribeSubscriberInput](unsubscribeSubscriberStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the unsubscribed email identity."
type completeSubscriberUnsubscribe struct {
	dex.StepDefaultsNoWaitFor[campaignmonitor.UnsubscribeSubscriberResult]
}

func (completeSubscriberUnsubscribe) GetStepType() string { return "CompleteUnsubscribeSubscriber" }
func (completeSubscriberUnsubscribe) Execute(ctx dex.Context, result campaignmonitor.UnsubscribeSubscriberResult) (*dex.StepDecision, error) {
	if err := unsubscribedSubscriberAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SubscriberUnsubscribeFlow)(nil)
