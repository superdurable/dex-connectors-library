// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flow

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/contentful/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const assetStepType = "CallContentfulAsset"

var assetResultAttribute = dex.DefineAttribute[contentful.ContentfulReadOutput]("contentful-asset-result")

// AssetFlow reads one published Contentful asset and stores its result.
type AssetFlow struct {
	dex.FlowDefaults
	connection contentful.Connection
}

// NewAssetFlow binds the Contentful connection for one asset request.
func NewAssetFlow(connection contentful.Connection) *AssetFlow {
	return &AssetFlow{connection: connection}
}

// GetFlowType gives the asset inspection Flow a stable identity.
func (*AssetFlow) GetFlowType() string { return "ContentfulAssetInspection" }

// GetPersistenceSchema registers the retained asset result.
func (*AssetFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{assetResultAttribute}}
}

// GetRPCs exposes read-only asset views in Dex Web.
func (flow *AssetFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:contentful-asset-result value-type:json editable:false description:"Published asset"
// GetDexSummary shows the published asset after the Flow completes.
func (*AssetFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	asset, err := readAsset(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"contentful-asset-result": asset}}, nil
}

// dex:field attribute-key:contentful-asset-result value-type:json editable:false description:"Published asset"
// GetDexDisplay shows the published asset after the Flow completes.
func (*AssetFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	asset, err := readAsset(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"contentful-asset-result": asset}}, nil
}

func readAsset(ctx dex.Context) (contentful.ContentfulReadOutput, error) {
	asset, err := assetResultAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return contentful.ContentfulReadOutput{}, nil
	}
	return asset, err
}

// GetSteps wires the operation-specific asset Query to completion.
func (flow *AssetFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAsset{}),
		dex.DefineStep(contentful.NewGetAssetStep(contentful.GetAssetStepConfig[contentful.GetAssetInput]{
			StepType: assetStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "contentful", GroupLabel: "Contentful", Explanation: "Read one published Contentful asset."},
			MapToOperationInput: mapAssetInput, Found: sdkgo.GoTo(completeAsset{}),
		})),
		dex.DefineStep(completeAsset{}),
	}
}

func mapAssetInput(input contentful.GetAssetInput) contentful.GetAssetInput { return input }

// dex:group group-id:contentful-start group-label:"Contentful asset"
// dex:explanation text:"Pass the asset ID to the provider Query."
type startAsset struct{ dex.StepDefaults }

func (startAsset) WaitFor(dex.Context, contentful.GetAssetInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAsset) Execute(_ dex.Context, input contentful.GetAssetInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[contentful.GetAssetInput](assetStepType), input), nil
}

// dex:group group-id:contentful-complete group-label:"Contentful asset"
// dex:explanation text:"Store the published asset and complete the Flow."
type completeAsset struct {
	dex.StepDefaultsNoWaitFor[contentful.GetAssetResult]
}

func (completeAsset) Execute(ctx dex.Context, result contentful.GetAssetResult) (*dex.StepDecision, error) {
	if err := assetResultAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AssetFlow)(nil)
