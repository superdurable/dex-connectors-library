// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/keap/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ProductFlowType identifies the product lookup example Flow.
const ProductFlowType = "KeapProductLookup"

const getProductStepType = "CallGetProduct"

var productProfileAttribute = dex.DefineAttribute[keap.ProductProfile]("keap-product-profile")

// ProductFlow reads one Keap product.
type ProductFlow struct {
	dex.FlowDefaults
	connection keap.Connection
}

// NewProductFlow binds the static Keap connection at registration.
func NewProductFlow(connection keap.Connection) *ProductFlow { return &ProductFlow{connection: connection} }

func (*ProductFlow) GetFlowType() string { return ProductFlowType }

func (*ProductFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{productProfileAttribute}}
}

func (flow *ProductFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startProductLookup{}),
		dex.DefineStep(keap.NewGetProductStep(keap.GetProductStepConfig[keap.ProductIDInput]{
			StepType: getProductStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Keap", Explanation: "Read one CRM product."},
			Connection:  flow.connection, MapToOperationInput: mapProductID,
			Found: sdkgo.GoTo(completeProductLookup{}),
		})),
		dex.DefineStep(completeProductLookup{}),
	}
}

func (flow *ProductFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:keap-product-profile value-type:json editable:false description:"Product profile"
// GetDexSummary shows the product in Dex Web.
func (*ProductFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	product, err := readProductProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-product-profile": product}}, nil
}

// dex:field attribute-key:keap-product-profile value-type:json editable:false description:"Product profile"
// GetDexDisplay shows the product in Dex Web.
func (*ProductFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	product, err := readProductProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-product-profile": product}}, nil
}

func readProductProfile(ctx dex.Context) (keap.ProductProfile, error) {
	product, err := productProfileAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return keap.ProductProfile{}, nil
	}
	return product, err
}

func mapProductID(input keap.ProductIDInput) keap.ProductIDInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected product ID to the provider Step."
type startProductLookup struct{ dex.StepDefaults }

func (startProductLookup) GetStepType() string { return "StartProductLookup" }
func (startProductLookup) WaitFor(dex.Context, keap.ProductIDInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startProductLookup) Execute(_ dex.Context, input keap.ProductIDInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[keap.ProductIDInput](getProductStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the returned product and complete the Flow."
type completeProductLookup struct {
	dex.StepDefaultsNoWaitFor[keap.GetProductResult]
}

func (completeProductLookup) GetStepType() string { return "CompleteProductLookup" }
func (completeProductLookup) Execute(ctx dex.Context, result keap.GetProductResult) (*dex.StepDecision, error) {
	if err := productProfileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProductFlow)(nil)
