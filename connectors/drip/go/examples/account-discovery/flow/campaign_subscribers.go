// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/drip/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CampaignSubscribersFlowType is the stable campaign subscriber page identity.
const CampaignSubscribersFlowType = "DripCampaignSubscribers"

var campaignSubscribersAttribute = dex.DefineAttribute[drip.SubscribersPage]("drip-campaign-subscribers")

// CampaignSubscribersFlow reads one bounded page of campaign subscribers.
type CampaignSubscribersFlow struct {
	dex.FlowDefaults
	connection drip.Connection
}

// NewCampaignSubscribersFlow binds the provider connection at registration time.
func NewCampaignSubscribersFlow(connection drip.Connection) *CampaignSubscribersFlow {
	return &CampaignSubscribersFlow{connection: connection}
}

// GetFlowType preserves the example's Flow identity.
func (*CampaignSubscribersFlow) GetFlowType() string { return CampaignSubscribersFlowType }

// GetPersistenceSchema registers the subscriber page Attribute.
func (*CampaignSubscribersFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignSubscribersAttribute}}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *CampaignSubscribersFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:drip-campaign-subscribers value-type:json editable:false description:"Campaign subscribers"
// GetDexSummary shows the last campaign subscriber page in Dex Web.
func (*CampaignSubscribersFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCampaignSubscribers(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"drip-campaign-subscribers": page}}, nil
}

// dex:field attribute-key:drip-campaign-subscribers value-type:json editable:false description:"Campaign subscribers"
// GetDexDisplay shows the last campaign subscriber page in Dex Web.
func (*CampaignSubscribersFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCampaignSubscribers(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"drip-campaign-subscribers": page}}, nil
}

func readCampaignSubscribers(ctx dex.Context) (drip.SubscribersPage, error) {
	page, err := campaignSubscribersAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return drip.SubscribersPage{}, nil
	}
	return page, err
}

// GetSteps registers the operation-specific campaign subscriber Step.
func (flow *CampaignSubscribersFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCampaignSubscribers{}),
		dex.DefineStep(drip.NewListCampaignSubscribersStep(drip.ListCampaignSubscribersStepConfig[drip.ListCampaignSubscribersInput]{
			StepType: "CallListCampaignSubscribers", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Drip", Explanation: "Read one campaign subscriber page from Drip."},
			Connection:          flow.connection,
			MapToOperationInput: mapCampaignSubscribersInput,
			Listed:              sdkgo.GoTo(completeCampaignSubscribers{}),
		})),
		dex.DefineStep(completeCampaignSubscribers{}),
	}
}

func mapCampaignSubscribersInput(input drip.ListCampaignSubscribersInput) drip.ListCampaignSubscribersInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the campaign subscriber page request to Drip."
type startCampaignSubscribers struct{ dex.StepDefaults }

func (startCampaignSubscribers) GetStepType() string { return "StartCampaignSubscribers" }
func (startCampaignSubscribers) WaitFor(dex.Context, drip.ListCampaignSubscribersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCampaignSubscribers) Execute(_ dex.Context, input drip.ListCampaignSubscribersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[drip.ListCampaignSubscribersInput]("CallListCampaignSubscribers"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the selected campaign subscriber page."
type completeCampaignSubscribers struct {
	dex.StepDefaultsNoWaitFor[drip.ListCampaignSubscribersResult]
}

func (completeCampaignSubscribers) GetStepType() string { return "CompleteCampaignSubscribers" }
func (completeCampaignSubscribers) Execute(ctx dex.Context, result drip.ListCampaignSubscribersResult) (*dex.StepDecision, error) {
	if err := campaignSubscribersAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CampaignSubscribersFlow)(nil)
