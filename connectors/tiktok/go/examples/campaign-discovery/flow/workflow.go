// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package campaigndiscovery runs TikTok listCampaigns as one Dex Flow.
package campaigndiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/tiktok/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TikTokCampaignDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "tiktok-business"
	operationStepType = "CallListCampaigns"
)

var campaignsAttribute = dex.DefineAttribute[tiktok.CampaignsPage]("tiktok-campaigns")

// Flow runs ListCampaigns once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection tiktok.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{campaignsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(tiktok.NewListCampaignsStep(tiktok.ListCampaignsStepConfig[tiktok.ListCampaignsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "List one page of TikTok campaigns."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-campaigns value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-campaigns": outcome}}, nil
}

// dex:field attribute-key:tiktok-campaigns value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-campaigns": outcome}}, nil
}

func readCampaigns(ctx dex.Context) (tiktok.CampaignsPage, error) {
	outcome, err := campaignsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.CampaignsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input tiktok.ListCampaignsInput) tiktok.ListCampaignsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListCampaigns" }
func (startRequest) WaitFor(dex.Context, tiktok.ListCampaignsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input tiktok.ListCampaignsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.ListCampaignsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[tiktok.ListCampaignsResult]
}

func (completeResult) GetStepType() string { return "CompleteListCampaigns" }
func (completeResult) Execute(ctx dex.Context, result tiktok.ListCampaignsResult) (*dex.StepDecision, error) {
	if err := campaignsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
