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
	// AdsFlowType is the stable example Flow identity.
	AdsFlowType = "TikTokAdsDiscovery"
	adsStepType = "CallListAds"
)

var adsAttribute = dex.DefineAttribute[tiktok.AdsPage]("tiktok-ads")

// AdsFlow runs ListAds once and completes with its typed output.
type AdsFlow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewAdsFlow binds the provider connection at registration time.
func NewAdsFlow(connection tiktok.Connection) *AdsFlow { return &AdsFlow{connection: connection} }

func (*AdsFlow) GetFlowType() string { return AdsFlowType }

func (*AdsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{adsAttribute}}
}

func (flow *AdsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAdsRequest{}),
		dex.DefineStep(tiktok.NewListAdsStep(tiktok.ListAdsStepConfig[tiktok.ListAdsInput]{
			StepType: adsStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "List one page of TikTok ads."},
			Connection:          flow.connection,
			MapToOperationInput: mapAdsInput,
			Listed:              sdkgo.GoTo(completeAdsResult{}),
		})),
		dex.DefineStep(completeAdsResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *AdsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-ads value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*AdsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAds(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-ads": outcome}}, nil
}

// dex:field attribute-key:tiktok-ads value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*AdsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAds(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-ads": outcome}}, nil
}

func readAds(ctx dex.Context) (tiktok.AdsPage, error) {
	outcome, err := adsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.AdsPage{}, nil
	}
	return outcome, err
}

// mapAdsInput preserves the validated typed start input for the provider call.
func mapAdsInput(input tiktok.ListAdsInput) tiktok.ListAdsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startAdsRequest struct{ dex.StepDefaults }

func (startAdsRequest) GetStepType() string { return "StartListAds" }
func (startAdsRequest) WaitFor(dex.Context, tiktok.ListAdsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAdsRequest) Execute(_ dex.Context, input tiktok.ListAdsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.ListAdsInput](adsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeAdsResult struct {
	dex.StepDefaultsNoWaitFor[tiktok.ListAdsResult]
}

func (completeAdsResult) GetStepType() string { return "CompleteListAds" }
func (completeAdsResult) Execute(ctx dex.Context, result tiktok.ListAdsResult) (*dex.StepDecision, error) {
	if err := adsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AdsFlow)(nil)
