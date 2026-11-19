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
	// UpdateCampaignFlowType identifies this runnable campaign Flow.
	updateCampaignFlowType = "TikTokUpdateCampaign"
	updateCampaignStepType = "CallUpdateCampaign"
)

var updateCampaignAttribute = dex.DefineAttribute[tiktok.CampaignWriteResult]("tiktok-updated-campaign")

// UpdateCampaignFlow updates one advertiser campaign.
type UpdateCampaignFlow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewUpdateCampaignFlow binds the provider connection at registration time.
func NewUpdateCampaignFlow(connection tiktok.Connection) *UpdateCampaignFlow {
	return &UpdateCampaignFlow{connection: connection}
}

func (*UpdateCampaignFlow) GetFlowType() string { return updateCampaignFlowType }

func (*UpdateCampaignFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updateCampaignAttribute}}
}

func (flow *UpdateCampaignFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUpdateCampaign{}),
		dex.DefineStep(tiktok.NewUpdateCampaignStep(tiktok.UpdateCampaignStepConfig[tiktok.UpdateCampaignInput]{
			StepType: updateCampaignStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "Update one campaign."},
			MapToOperationInput: mapUpdateCampaignInput, Updated: sdkgo.GoTo(completeUpdateCampaign{}),
		})),
		dex.DefineStep(completeUpdateCampaign{}),
	}
}

func (flow *UpdateCampaignFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-updated-campaign value-type:json editable:false description:"Campaign ID"
// GetDexSummary shows the confirmed campaign ID.
func (*UpdateCampaignFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readUpdateCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-updated-campaign": result}}, nil
}

// dex:field attribute-key:tiktok-updated-campaign value-type:json editable:false description:"Campaign ID"
// GetDexDisplay shows the confirmed campaign ID.
func (*UpdateCampaignFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readUpdateCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-updated-campaign": result}}, nil
}

func readUpdateCampaign(ctx dex.Context) (tiktok.CampaignWriteResult, error) {
	result, err := updateCampaignAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.CampaignWriteResult{}, nil
	}
	return result, err
}

func mapUpdateCampaignInput(input tiktok.UpdateCampaignInput) tiktok.UpdateCampaignInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the advertiser and campaign settings to TikTok."
type startUpdateCampaign struct{ dex.StepDefaults }

func (startUpdateCampaign) GetStepType() string { return "StartUpdateCampaign" }
func (startUpdateCampaign) WaitFor(dex.Context, tiktok.UpdateCampaignInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUpdateCampaign) Execute(_ dex.Context, input tiktok.UpdateCampaignInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.UpdateCampaignInput](updateCampaignStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after TikTok confirms the campaign ID."
type completeUpdateCampaign struct {
	dex.StepDefaultsNoWaitFor[tiktok.UpdateCampaignResult]
}

func (completeUpdateCampaign) GetStepType() string { return "CompleteUpdateCampaign" }
func (completeUpdateCampaign) Execute(ctx dex.Context, result tiktok.UpdateCampaignResult) (*dex.StepDecision, error) {
	if err := updateCampaignAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpdateCampaignFlow)(nil)
