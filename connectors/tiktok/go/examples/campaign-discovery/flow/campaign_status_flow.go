// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigndiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/tiktok/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// CampaignStatusFlowType identifies this runnable status Flow.
	campaignStatusFlowType = "TikTokCampaignStatus"
	campaignStatusStepType = "CallUpdateCampaignStatus"
)

var campaignStatusAttribute = dex.DefineAttribute[tiktok.AdvertisingStatusResult]("tiktok-campaign-status")

// CampaignStatusFlow sets the operation status of one campaign.
type CampaignStatusFlow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewCampaignStatusFlow binds the provider connection at registration time.
func NewCampaignStatusFlow(connection tiktok.Connection) *CampaignStatusFlow {
	return &CampaignStatusFlow{connection: connection}
}

func (*CampaignStatusFlow) GetFlowType() string { return campaignStatusFlowType }

func (*CampaignStatusFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignStatusAttribute}}
}

func (flow *CampaignStatusFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCampaignStatus{}),
		dex.DefineStep(tiktok.NewUpdateCampaignStatusStep(tiktok.UpdateCampaignStatusStepConfig[tiktok.UpdateCampaignStatusInput]{
			StepType: campaignStatusStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "Set campaign operation status."},
			MapToOperationInput: mapCampaignStatusInput, Updated: sdkgo.GoTo(completeCampaignStatus{}),
		})),
		dex.DefineStep(completeCampaignStatus{}),
	}
}

func (flow *CampaignStatusFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-campaign-status value-type:json editable:false description:"Confirmed status"
// GetDexSummary shows the confirmed status.
func (*CampaignStatusFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCampaignStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-campaign-status": result}}, nil
}

// dex:field attribute-key:tiktok-campaign-status value-type:json editable:false description:"Confirmed status"
// GetDexDisplay shows the confirmed status.
func (*CampaignStatusFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCampaignStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-campaign-status": result}}, nil
}

func readCampaignStatus(ctx dex.Context) (tiktok.AdvertisingStatusResult, error) {
	result, err := campaignStatusAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.AdvertisingStatusResult{}, nil
	}
	return result, err
}

func mapCampaignStatusInput(input tiktok.UpdateCampaignStatusInput) tiktok.UpdateCampaignStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected campaign and absolute status to TikTok."
type startCampaignStatus struct{ dex.StepDefaults }

func (startCampaignStatus) GetStepType() string { return "StartUpdateCampaignStatus" }
func (startCampaignStatus) WaitFor(dex.Context, tiktok.UpdateCampaignStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCampaignStatus) Execute(_ dex.Context, input tiktok.UpdateCampaignStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.UpdateCampaignStatusInput](campaignStatusStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after TikTok confirms the absolute status."
type completeCampaignStatus struct {
	dex.StepDefaultsNoWaitFor[tiktok.UpdateCampaignStatusResult]
}

func (completeCampaignStatus) GetStepType() string { return "CompleteUpdateCampaignStatus" }
func (completeCampaignStatus) Execute(ctx dex.Context, result tiktok.UpdateCampaignStatusResult) (*dex.StepDecision, error) {
	if err := campaignStatusAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CampaignStatusFlow)(nil)
