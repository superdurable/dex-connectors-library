// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerread

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/wave/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CreateProductFlowType identifies the product creation example.
const CreateProductFlowType = "WaveCreateProduct"

var createdProductAttribute = dex.DefineAttribute[wave.ProductProfile]("wave-created-product")

// CreateProductFlow creates a priced product in a Wave business.
type CreateProductFlow struct {
	dex.FlowDefaults
	connection wave.Connection
}

// NewCreateProductFlow binds the static Wave connection.
func NewCreateProductFlow(connection wave.Connection) *CreateProductFlow {
	return &CreateProductFlow{connection: connection}
}

// GetFlowType returns the stable product creation Flow identity.
func (*CreateProductFlow) GetFlowType() string { return CreateProductFlowType }

// GetPersistenceSchema registers the created product Attribute.
func (*CreateProductFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdProductAttribute}}
}

// GetSteps registers the start, create, and completion Steps.
func (flow *CreateProductFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateProduct{}),
		dex.DefineStep(wave.NewCreateProductStep(wave.CreateProductStepConfig[wave.CreateProductInput]{
			StepType: "CallCreateProduct", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Wave", Explanation: "Create one priced Wave product."},
			MapToOperationInput: mapCreateProductInput, Created: sdkgo.GoTo(completeCreateProduct{}),
		})),
		dex.DefineStep(completeCreateProduct{}),
	}
}

// GetRPCs registers read-only Dex Web views.
func (flow *CreateProductFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:wave-created-product value-type:json editable:false description:"Created product"
// GetDexSummary returns the created product when available.
func (*CreateProductFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedProduct(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wave-created-product": value}}, nil
}

// dex:field attribute-key:wave-created-product value-type:json editable:false description:"Created product"
// GetDexDisplay returns the created product when available.
func (*CreateProductFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedProduct(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wave-created-product": value}}, nil
}

func readCreatedProduct(ctx dex.Context) (wave.ProductProfile, error) {
	value, err := createdProductAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wave.ProductProfile{}, nil
	}
	return value, err
}
func mapCreateProductInput(input wave.CreateProductInput) wave.CreateProductInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed product details to the provider Step."
type startCreateProduct struct{ dex.StepDefaults }

func (startCreateProduct) GetStepType() string { return "StartWaveProductCreation" }
func (startCreateProduct) WaitFor(dex.Context, wave.CreateProductInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateProduct) Execute(_ dex.Context, input wave.CreateProductInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wave.CreateProductInput]("CallCreateProduct"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created product metadata."
type completeCreateProduct struct {
	dex.StepDefaultsNoWaitFor[wave.CreateProductResult]
}

func (completeCreateProduct) GetStepType() string { return "CompleteWaveProductCreation" }
func (completeCreateProduct) Execute(ctx dex.Context, result wave.CreateProductResult) (*dex.StepDecision, error) {
	if err := createdProductAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateProductFlow)(nil)
