// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package featurediscovery runs Productboard listFeatures as one Dex Flow.
package featurediscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/productboard/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ProductboardFeatureDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "productboard"
	operationStepType = "CallListFeatures"
)

var featuresAttribute = dex.DefineAttribute[productboard.FeaturesPage]("productboard-features")

// Flow runs ListFeatures once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection productboard.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection productboard.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{featuresAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(productboard.NewListFeaturesStep(productboard.ListFeaturesStepConfig[productboard.ListFeaturesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Productboard", Explanation: "List one page of Productboard features."},
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

// dex:field attribute-key:productboard-features value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readFeatures(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"productboard-features": outcome}}, nil
}

// dex:field attribute-key:productboard-features value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readFeatures(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"productboard-features": outcome}}, nil
}

func readFeatures(ctx dex.Context) (productboard.FeaturesPage, error) {
	outcome, err := featuresAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return productboard.FeaturesPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input productboard.ListFeaturesInput) productboard.ListFeaturesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListFeatures" }
func (startRequest) WaitFor(dex.Context, productboard.ListFeaturesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input productboard.ListFeaturesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[productboard.ListFeaturesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[productboard.ListFeaturesResult]
}

func (completeResult) GetStepType() string { return "CompleteListFeatures" }
func (completeResult) Execute(ctx dex.Context, result productboard.ListFeaturesResult) (*dex.StepDecision, error) {
	if err := featuresAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
