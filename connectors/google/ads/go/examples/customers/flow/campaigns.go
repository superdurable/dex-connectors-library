// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customers

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/google/ads/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CampaignFlowType is the stable identity of the campaign listing example.
const CampaignFlowType = "GoogleAdsCampaignsList"

const campaignStepType = "CallListCampaigns"

var campaignsAttribute = dex.DefineAttribute[ads.CampaignPage]("google-ads-campaigns-page")

// CampaignFlow lists campaigns for one Ads customer.
type CampaignFlow struct {
	dex.FlowDefaults
	connection ads.Connection
}

// NewCampaignFlow binds the provider connection at registration time.
func NewCampaignFlow(connection ads.Connection) *CampaignFlow {
	return &CampaignFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*CampaignFlow) GetFlowType() string { return CampaignFlowType }

// GetPersistenceSchema registers the returned page Attribute.
func (*CampaignFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignsAttribute}}
}

// GetSteps registers start, provider, and completion Steps.
func (flow *CampaignFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCampaignList{}),
		dex.DefineStep(ads.NewListCampaignsStep(ads.ListCampaignsStepConfig[ads.ListCampaignsInput]{
			StepType: campaignStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Ads", Explanation: "List a page of campaigns with performance totals for one Ads customer."},
			MapToOperationInput: func(input ads.ListCampaignsInput) ads.ListCampaignsInput { return input },
			Listed:              sdkgo.GoTo(completeCampaignList{}),
		})),
		dex.DefineStep(completeCampaignList{}),
	}
}

// GetRPCs registers read-only Dex Web result views.
func (flow *CampaignFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-ads-campaigns-page value-type:json editable:false description:"Campaign page"
// GetDexSummary returns the last campaign page when available.
func (*CampaignFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCampaignPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-ads-campaigns-page": page}}, nil
}

// dex:field attribute-key:google-ads-campaigns-page value-type:json editable:false description:"Campaign page"
// GetDexDisplay returns the last campaign page when available.
func (*CampaignFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCampaignPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-ads-campaigns-page": page}}, nil
}

func readCampaignPage(ctx dex.Context) (ads.CampaignPage, error) {
	page, err := campaignsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ads.CampaignPage{}, nil
	}
	return page, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the customer and filters to the campaign Step."
type startCampaignList struct{ dex.StepDefaults }

func (startCampaignList) GetStepType() string { return "StartListCampaigns" }
func (startCampaignList) WaitFor(dex.Context, ads.ListCampaignsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCampaignList) Execute(_ dex.Context, input ads.ListCampaignsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ads.ListCampaignsInput](campaignStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the campaign page."
type completeCampaignList struct {
	dex.StepDefaultsNoWaitFor[ads.ListCampaignsResult]
}

func (completeCampaignList) GetStepType() string { return "CompleteListCampaigns" }
func (completeCampaignList) Execute(ctx dex.Context, result ads.ListCampaignsResult) (*dex.StepDecision, error) {
	if err := campaignsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CampaignFlow)(nil)
