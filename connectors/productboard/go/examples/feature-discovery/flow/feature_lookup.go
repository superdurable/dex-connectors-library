// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package featurediscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/productboard/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FeatureLookupFlowType is the stable Flow identity for one feature lookup.
const FeatureLookupFlowType = "ProductboardFeatureLookup"

const lookupStepType = "CallGetFeature"

var featureAttribute = dex.DefineAttribute[productboard.FeatureMetadata]("productboard-feature")

// FeatureLookupFlow reads one feature through its operation-specific Step.
type FeatureLookupFlow struct {
	dex.FlowDefaults
	connection productboard.Connection
}

// NewFeatureLookupFlow binds the provider connection at registration time.
func NewFeatureLookupFlow(connection productboard.Connection) *FeatureLookupFlow {
	return &FeatureLookupFlow{connection: connection}
}

// GetFlowType preserves the stable example Flow identity.
func (*FeatureLookupFlow) GetFlowType() string { return FeatureLookupFlowType }

// GetPersistenceSchema registers the result Attribute.
func (*FeatureLookupFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{featureAttribute}}
}

// GetSteps registers the start, provider lookup, and completion Steps.
func (flow *FeatureLookupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startFeatureLookup{}),
		dex.DefineStep(productboard.NewGetFeatureStep(productboard.GetFeatureStepConfig[productboard.GetFeatureInput]{
			StepType: lookupStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Productboard", Explanation: "Read one Productboard feature by ID."},
			Connection:          flow.connection,
			MapToOperationInput: mapFeatureLookupInput,
			Found:               sdkgo.GoTo(completeFeatureLookup{}),
		})),
		dex.DefineStep(completeFeatureLookup{}),
	}
}

// GetRPCs registers read-only Dex Web views.
func (flow *FeatureLookupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:productboard-feature value-type:json editable:false description:"Feature metadata"
// GetDexSummary shows the last feature lookup in Dex Web.
func (*FeatureLookupFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	feature, err := readFeature(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"productboard-feature": feature}}, nil
}

// dex:field attribute-key:productboard-feature value-type:json editable:false description:"Feature metadata"
// GetDexDisplay shows the last feature lookup in Dex Web.
func (*FeatureLookupFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	feature, err := readFeature(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"productboard-feature": feature}}, nil
}

func readFeature(ctx dex.Context) (productboard.FeatureMetadata, error) {
	feature, err := featureAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return productboard.FeatureMetadata{}, nil
	}
	return feature, err
}

func mapFeatureLookupInput(input productboard.GetFeatureInput) productboard.GetFeatureInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested feature ID to the provider Step."
type startFeatureLookup struct{ dex.StepDefaults }

func (startFeatureLookup) GetStepType() string { return "StartFeatureLookup" }
func (startFeatureLookup) WaitFor(dex.Context, productboard.GetFeatureInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startFeatureLookup) Execute(_ dex.Context, input productboard.GetFeatureInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[productboard.GetFeatureInput](lookupStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one feature's metadata."
type completeFeatureLookup struct {
	dex.StepDefaultsNoWaitFor[productboard.GetFeatureResult]
}

func (completeFeatureLookup) GetStepType() string { return "CompleteFeatureLookup" }
func (completeFeatureLookup) Execute(ctx dex.Context, result productboard.GetFeatureResult) (*dex.StepDecision, error) {
	if err := featureAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*FeatureLookupFlow)(nil)
