// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/drip/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CampaignPageFlowType is the stable campaign page example identity.
const CampaignPageFlowType = "DripCampaignPage"

var campaignPageAttribute = dex.DefineAttribute[drip.CampaignsPage]("drip-campaigns-page")

// CampaignPageFlow reads one bounded page of Drip campaign identities and statuses.
type CampaignPageFlow struct {
	dex.FlowDefaults
	connection drip.Connection
}

// NewCampaignPageFlow binds the provider connection at registration time.
func NewCampaignPageFlow(connection drip.Connection) *CampaignPageFlow {
	return &CampaignPageFlow{connection: connection}
}

// GetFlowType preserves the campaign page Flow identity.
func (*CampaignPageFlow) GetFlowType() string { return CampaignPageFlowType }

// GetPersistenceSchema registers the campaign page Attribute.
func (*CampaignPageFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignPageAttribute}}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *CampaignPageFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:drip-campaigns-page value-type:json editable:false description:"Campaign page"
// GetDexSummary shows the last campaign page in Dex Web.
func (*CampaignPageFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCampaignPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"drip-campaigns-page": page}}, nil
}

// dex:field attribute-key:drip-campaigns-page value-type:json editable:false description:"Campaign page"
// GetDexDisplay shows the last campaign page in Dex Web.
func (*CampaignPageFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCampaignPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"drip-campaigns-page": page}}, nil
}

func readCampaignPage(ctx dex.Context) (drip.CampaignsPage, error) {
	page, err := campaignPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return drip.CampaignsPage{}, nil
	}
	return page, err
}

// GetSteps registers the operation-specific campaign page Step.
func (flow *CampaignPageFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCampaignPage{}),
		dex.DefineStep(drip.NewListCampaignsStep(drip.ListCampaignsStepConfig[drip.ListCampaignsInput]{
			StepType: "CallListCampaigns", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Drip", Explanation: "Read one campaign page from Drip."},
			Connection:          flow.connection,
			MapToOperationInput: mapCampaignPageInput,
			Listed:              sdkgo.GoTo(completeCampaignPage{}),
		})),
		dex.DefineStep(completeCampaignPage{}),
	}
}

func mapCampaignPageInput(input drip.ListCampaignsInput) drip.ListCampaignsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the campaign page request to Drip."
type startCampaignPage struct{ dex.StepDefaults }

func (startCampaignPage) GetStepType() string { return "StartCampaignPage" }
func (startCampaignPage) WaitFor(dex.Context, drip.ListCampaignsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCampaignPage) Execute(_ dex.Context, input drip.ListCampaignsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[drip.ListCampaignsInput]("CallListCampaigns"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the selected campaign page."
type completeCampaignPage struct {
	dex.StepDefaultsNoWaitFor[drip.ListCampaignsResult]
}

func (completeCampaignPage) GetStepType() string { return "CompleteCampaignPage" }
func (completeCampaignPage) Execute(ctx dex.Context, result drip.ListCampaignsResult) (*dex.StepDecision, error) {
	if err := campaignPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CampaignPageFlow)(nil)
