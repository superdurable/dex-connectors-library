// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package adinspection runs Meta Ads Insights getAd as one Dex Flow.
package adinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/ads-insights/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MetaAdsInsightsAdInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "meta-ads-insights"
	operationStepType = "CallGetAd"
)

var adMetadataAttribute = dex.DefineAttribute[metaadsinsights.AdMetadata]("meta-ads-insights-ad-metadata")

// Flow runs GetAd once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection metaadsinsights.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection metaadsinsights.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{adMetadataAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(metaadsinsights.NewGetAdStep(metaadsinsights.GetAdStepConfig[metaadsinsights.GetAdInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Meta Ads Insights", Explanation: "Call the provider's getAd operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:meta-ads-insights-ad-metadata value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAdMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meta-ads-insights-ad-metadata": outcome}}, nil
}

// dex:field attribute-key:meta-ads-insights-ad-metadata value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAdMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meta-ads-insights-ad-metadata": outcome}}, nil
}

func readAdMetadata(ctx dex.Context) (metaadsinsights.AdMetadata, error) {
	outcome, err := adMetadataAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return metaadsinsights.AdMetadata{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input metaadsinsights.GetAdInput) metaadsinsights.GetAdInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetAd" }
func (startRequest) WaitFor(dex.Context, metaadsinsights.GetAdInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input metaadsinsights.GetAdInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[metaadsinsights.GetAdInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[metaadsinsights.GetAdResult]
}

func (completeResult) GetStepType() string { return "CompleteGetAd" }
func (completeResult) Execute(ctx dex.Context, result metaadsinsights.GetAdResult) (*dex.StepDecision, error) {
	if err := adMetadataAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
