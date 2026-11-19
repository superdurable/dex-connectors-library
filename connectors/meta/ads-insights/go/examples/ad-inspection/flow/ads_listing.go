// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/ads-insights/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listAdsStepType = "CallListAds"

var adsPageAttribute = dex.DefineAttribute[metaadsinsights.MarketingObjectsPage]("meta-ads-insights-ads-page")

// AdsListingFlow reads one page of an account's ads.
type AdsListingFlow struct {
	dex.FlowDefaults
	connection metaadsinsights.Connection
}

// NewAdsListingFlow binds the static Marketing API connection.
func NewAdsListingFlow(connection metaadsinsights.Connection) *AdsListingFlow {
	return &AdsListingFlow{connection: connection}
}

// GetFlowType identifies the ads listing Flow.
func (*AdsListingFlow) GetFlowType() string { return "MetaAdsInsightsAdsListing" }

// GetPersistenceSchema registers the durable listing result.
func (*AdsListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{adsPageAttribute}}
}

// GetSteps wires the operation-specific ListAds Query.
func (flow *AdsListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAdsListing{}),
		dex.DefineStep(metaadsinsights.NewListAdsStep(metaadsinsights.ListAdsStepConfig[metaadsinsights.ListMarketingObjectsInput]{
			StepType: listAdsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Meta Ads Insights", Explanation: "List one page of ads in the selected ad account."},
			Connection:  flow.connection, MapToOperationInput: adsListingInput,
			Listed: sdkgo.GoTo(completeAdsListing{}),
		})),
		dex.DefineStep(completeAdsListing{}),
	}
}

// GetRPCs exposes the last ads page in Dex Web.
func (flow *AdsListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:meta-ads-insights-ads-page value-type:json editable:false description:"Ad account ads page"
// GetDexSummary returns the last ads page.
func (*AdsListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readAdsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meta-ads-insights-ads-page": page}}, nil
}

// dex:field attribute-key:meta-ads-insights-ads-page value-type:json editable:false description:"Ad account ads page"
// GetDexDisplay returns the last ads page.
func (*AdsListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readAdsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meta-ads-insights-ads-page": page}}, nil
}

func readAdsPage(ctx dex.Context) (metaadsinsights.MarketingObjectsPage, error) {
	page, err := adsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return metaadsinsights.MarketingObjectsPage{}, nil
	}
	return page, err
}

func adsListingInput(input metaadsinsights.ListMarketingObjectsInput) metaadsinsights.ListMarketingObjectsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the ad account ID and cursor to the provider Step."
type startAdsListing struct{ dex.StepDefaults }

func (startAdsListing) GetStepType() string { return "StartAdsListing" }
func (startAdsListing) WaitFor(dex.Context, metaadsinsights.ListMarketingObjectsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAdsListing) Execute(_ dex.Context, input metaadsinsights.ListMarketingObjectsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[metaadsinsights.ListMarketingObjectsInput](listAdsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the ads page and complete the listing Flow."
type completeAdsListing struct {
	dex.StepDefaultsNoWaitFor[metaadsinsights.ListAdsResult]
}

func (completeAdsListing) GetStepType() string { return "CompleteAdsListing" }
func (completeAdsListing) Execute(ctx dex.Context, result metaadsinsights.ListAdsResult) (*dex.StepDecision, error) {
	if err := adsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AdsListingFlow)(nil)
