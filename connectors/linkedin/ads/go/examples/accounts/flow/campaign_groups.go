// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/linkedin/ads/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CampaignGroupsFlow reads campaign groups in one LinkedIn ad account.
type CampaignGroupsFlow struct {
	dex.FlowDefaults
	connection ads.Connection
}

// NewCampaignGroupsFlow binds the provider connection at registration time.
func NewCampaignGroupsFlow(connection ads.Connection) *CampaignGroupsFlow {
	return &CampaignGroupsFlow{connection: connection}
}

func (*CampaignGroupsFlow) GetFlowType() string { return "LinkedInAdsCampaignGroupsList" }

var campaignGroupsAttribute = dex.DefineAttribute[ads.CampaignGroupsPage]("linkedin-campaign-groups-page")

func (*CampaignGroupsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignGroupsAttribute}}
}

func (flow *CampaignGroupsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCampaignGroups{}),
		dex.DefineStep(ads.NewListCampaignGroupsStep(ads.ListCampaignGroupsStepConfig[ads.ListCampaignGroupsInput]{
			StepType: "CallListCampaignGroups", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "LinkedIn Ads", Explanation: "Read campaign groups matching the selected account and status."},
			Connection:  flow.connection, MapToOperationInput: mapCampaignGroupsInput,
			Listed: sdkgo.GoTo(completeCampaignGroups{}),
		})),
		dex.DefineStep(completeCampaignGroups{}),
	}
}

func (flow *CampaignGroupsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:linkedin-campaign-groups-page value-type:json editable:false description:"Campaign groups"
// GetDexSummary shows the last campaign-group result in Dex Web.
func (*CampaignGroupsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCampaignGroups(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"linkedin-campaign-groups-page": value}}, nil
}

// dex:field attribute-key:linkedin-campaign-groups-page value-type:json editable:false description:"Campaign groups"
// GetDexDisplay shows the last campaign-group result in Dex Web.
func (*CampaignGroupsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCampaignGroups(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"linkedin-campaign-groups-page": value}}, nil
}

func readCampaignGroups(ctx dex.Context) (ads.CampaignGroupsPage, error) {
	value, err := campaignGroupsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ads.CampaignGroupsPage{}, nil
	}
	return value, err
}

func mapCampaignGroupsInput(input ads.ListCampaignGroupsInput) ads.ListCampaignGroupsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the account, status, and cursor to LinkedIn Ads."
type startCampaignGroups struct{ dex.StepDefaults }

func (startCampaignGroups) GetStepType() string { return "StartListCampaignGroups" }
func (startCampaignGroups) WaitFor(dex.Context, ads.ListCampaignGroupsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCampaignGroups) Execute(_ dex.Context, input ads.ListCampaignGroupsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ads.ListCampaignGroupsInput]("CallListCampaignGroups"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the selected campaign groups and complete the Flow."
type completeCampaignGroups struct {
	dex.StepDefaultsNoWaitFor[ads.ListCampaignGroupsResult]
}

func (completeCampaignGroups) GetStepType() string { return "CompleteListCampaignGroups" }
func (completeCampaignGroups) Execute(ctx dex.Context, result ads.ListCampaignGroupsResult) (*dex.StepDecision, error) {
	if err := campaignGroupsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CampaignGroupsFlow)(nil)
