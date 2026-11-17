// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigndiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/advertising/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listAdsStepType = "CallListAds"

var adsAttribute = dex.DefineAttribute[microsoftadvertising.AdsPage]("microsoft-advertising-ads")

// AdDiscoveryFlow reads responsive search ads in one ad group.
type AdDiscoveryFlow struct {
	dex.FlowDefaults
	connection microsoftadvertising.Connection
}

// NewAdDiscoveryFlow binds the static Microsoft Advertising connection.
func NewAdDiscoveryFlow(connection microsoftadvertising.Connection) *AdDiscoveryFlow {
	return &AdDiscoveryFlow{connection: connection}
}

// GetPersistenceSchema registers the ad page for Dex Web.
func (*AdDiscoveryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{adsAttribute}}
}

// GetSteps registers the request, provider, and completion Steps.
func (flow *AdDiscoveryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAdDiscovery{}),
		dex.DefineStep(microsoftadvertising.NewListAdsStep(microsoftadvertising.ListAdsStepConfig[microsoftadvertising.ListAdsInput]{
			StepType: listAdsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Microsoft Advertising", Explanation: "List responsive search ads in one ad group."},
			Connection:  flow.connection,
			MapToOperationInput: func(input microsoftadvertising.ListAdsInput) microsoftadvertising.ListAdsInput {
				return input
			},
			Listed: sdkgo.GoTo(completeAdDiscovery{}),
		})),
		dex.DefineStep(completeAdDiscovery{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *AdDiscoveryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:microsoft-advertising-ads value-type:json editable:false description:"Ads"
// GetDexSummary returns the last ad listing.
func (*AdDiscoveryFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	ads, err := readAds(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoft-advertising-ads": ads}}, nil
}

// dex:field attribute-key:microsoft-advertising-ads value-type:json editable:false description:"Ads"
// GetDexDisplay returns the listing for Dex Web.
func (*AdDiscoveryFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	ads, err := readAds(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"microsoft-advertising-ads": ads}}, nil
}

func readAds(ctx dex.Context) (microsoftadvertising.AdsPage, error) {
	ads, err := adsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return microsoftadvertising.AdsPage{}, nil
	}
	return ads, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the account and ad group IDs to the provider Step."
type startAdDiscovery struct{ dex.StepDefaults }

func (startAdDiscovery) WaitFor(dex.Context, microsoftadvertising.ListAdsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAdDiscovery) Execute(_ dex.Context, input microsoftadvertising.ListAdsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[microsoftadvertising.ListAdsInput](listAdsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the ad group's ads."
type completeAdDiscovery struct {
	dex.StepDefaultsNoWaitFor[microsoftadvertising.ListAdsResult]
}

func (completeAdDiscovery) Execute(ctx dex.Context, result microsoftadvertising.ListAdsResult) (*dex.StepDecision, error) {
	if err := adsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AdDiscoveryFlow)(nil)
