// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createEcommerceCustomerStepType = "CallCreateEcommerceCustomer"

var createdEcommerceCustomerAttribute = dex.DefineAttribute[activecampaign.EcommerceCustomerMetadata]("activecampaign-created-ecommerce-customer")

// CreateEcommerceCustomerFlow demonstrates a repeat-safe account create through a Dex Step.
type CreateEcommerceCustomerFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewCreateEcommerceCustomerFlow binds the same static connection as the inspection Flow.
func NewCreateEcommerceCustomerFlow(connection activecampaign.Connection) *CreateEcommerceCustomerFlow {
	return &CreateEcommerceCustomerFlow{connection: connection}
}

// GetFlowType is the stable create example identity.
func (*CreateEcommerceCustomerFlow) GetFlowType() string {
	return "ActiveCampaignCreateEcommerceCustomer"
}

// GetPersistenceSchema registers the created tag displayed in Dex Web.
func (*CreateEcommerceCustomerFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdEcommerceCustomerAttribute}}
}

// GetSteps registers the start, provider call, and completion boundary.
func (flow *CreateEcommerceCustomerFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateEcommerceCustomer{}),
		dex.DefineStep(activecampaign.NewCreateEcommerceCustomerStep(activecampaign.CreateEcommerceCustomerStepConfig[activecampaign.CreateEcommerceCustomerInput]{
			StepType: createEcommerceCustomerStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Create one ecommerce customer by name."},
			Connection:  flow.connection, MapToOperationInput: MapToCreateEcommerceCustomerInput,
			Created: sdkgo.GoTo(completeCreateEcommerceCustomer{}),
		})),
		dex.DefineStep(completeCreateEcommerceCustomer{}),
	}
}

// GetRPCs exposes the stored customer in Dex Web.
func (flow *CreateEcommerceCustomerFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-created-ecommerce-customer value-type:json editable:false description:"Created ecommerce customer"
// GetDexSummary shows the created customer in Dex Web.
func (*CreateEcommerceCustomerFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	customer, err := readCreatedEcommerceCustomer(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-ecommerce-customer": customer}}, nil
}

// dex:field attribute-key:activecampaign-created-ecommerce-customer value-type:json editable:false description:"Created ecommerce customer"
// GetDexDisplay shows the created customer in Dex Web.
func (*CreateEcommerceCustomerFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	customer, err := readCreatedEcommerceCustomer(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-ecommerce-customer": customer}}, nil
}

func readCreatedEcommerceCustomer(ctx dex.Context) (activecampaign.EcommerceCustomerMetadata, error) {
	customer, err := createdEcommerceCustomerAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.EcommerceCustomerMetadata{}, nil
	}
	return customer, err
}

// MapToCreateEcommerceCustomerInput preserves the typed start input for the provider call.
func MapToCreateEcommerceCustomerInput(input activecampaign.CreateEcommerceCustomerInput) activecampaign.CreateEcommerceCustomerInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the customer fields to the create Step."
type startCreateEcommerceCustomer struct{ dex.StepDefaults }

func (startCreateEcommerceCustomer) GetStepType() string { return "StartCreateEcommerceCustomer" }
func (startCreateEcommerceCustomer) WaitFor(dex.Context, activecampaign.CreateEcommerceCustomerInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateEcommerceCustomer) Execute(_ dex.Context, input activecampaign.CreateEcommerceCustomerInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.CreateEcommerceCustomerInput](createEcommerceCustomerStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created ecommerce customer metadata."
type completeCreateEcommerceCustomer struct {
	dex.StepDefaultsNoWaitFor[activecampaign.CreateEcommerceCustomerResult]
}

func (completeCreateEcommerceCustomer) GetStepType() string { return "CompleteCreateEcommerceCustomer" }
func (completeCreateEcommerceCustomer) Execute(ctx dex.Context, result activecampaign.CreateEcommerceCustomerResult) (*dex.StepDecision, error) {
	if err := createdEcommerceCustomerAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateEcommerceCustomerFlow)(nil)
