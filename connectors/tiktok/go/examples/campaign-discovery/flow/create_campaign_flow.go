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
	// CreateCampaignFlowType identifies this runnable campaign Flow.
	createCampaignFlowType = "TikTokCreateCampaign"
	createCampaignStepType = "CallCreateCampaign"
)

var createCampaignAttribute = dex.DefineAttribute[tiktok.CampaignWriteResult]("tiktok-created-campaign")

// CreateCampaignFlow creates one advertiser campaign.
type CreateCampaignFlow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewCreateCampaignFlow binds the provider connection at registration time.
func NewCreateCampaignFlow(connection tiktok.Connection) *CreateCampaignFlow {
	return &CreateCampaignFlow{connection: connection}
}

func (*CreateCampaignFlow) GetFlowType() string { return createCampaignFlowType }

func (*CreateCampaignFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createCampaignAttribute}}
}

func (flow *CreateCampaignFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateCampaign{}),
		dex.DefineStep(tiktok.NewCreateCampaignStep(tiktok.CreateCampaignStepConfig[tiktok.CreateCampaignInput]{
			StepType: createCampaignStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "Create one campaign."},
			MapToOperationInput: mapCreateCampaignInput, Created: sdkgo.GoTo(completeCreateCampaign{}),
		})),
		dex.DefineStep(completeCreateCampaign{}),
	}
}

func (flow *CreateCampaignFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-created-campaign value-type:json editable:false description:"Campaign ID"
// GetDexSummary shows the confirmed campaign ID.
func (*CreateCampaignFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCreateCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-created-campaign": result}}, nil
}

// dex:field attribute-key:tiktok-created-campaign value-type:json editable:false description:"Campaign ID"
// GetDexDisplay shows the confirmed campaign ID.
func (*CreateCampaignFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCreateCampaign(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-created-campaign": result}}, nil
}

func readCreateCampaign(ctx dex.Context) (tiktok.CampaignWriteResult, error) {
	result, err := createCampaignAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.CampaignWriteResult{}, nil
	}
	return result, err
}

func mapCreateCampaignInput(input tiktok.CreateCampaignInput) tiktok.CreateCampaignInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the advertiser and campaign settings to TikTok."
type startCreateCampaign struct{ dex.StepDefaults }

func (startCreateCampaign) GetStepType() string { return "StartCreateCampaign" }
func (startCreateCampaign) WaitFor(dex.Context, tiktok.CreateCampaignInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateCampaign) Execute(_ dex.Context, input tiktok.CreateCampaignInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.CreateCampaignInput](createCampaignStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after TikTok confirms the campaign ID."
type completeCreateCampaign struct {
	dex.StepDefaultsNoWaitFor[tiktok.CreateCampaignResult]
}

func (completeCreateCampaign) GetStepType() string { return "CompleteCreateCampaign" }
func (completeCreateCampaign) Execute(ctx dex.Context, result tiktok.CreateCampaignResult) (*dex.StepDecision, error) {
	if err := createCampaignAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateCampaignFlow)(nil)
