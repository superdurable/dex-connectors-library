// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// UpsertEcommerceOrderFlowType is the stable example identity for keyed store-order writes.
const UpsertEcommerceOrderFlowType = "ActiveCampaignUpsertEcommerceOrder"

const upsertEcommerceOrderStepType = "CallUpsertEcommerceOrder"

var upsertedOrderAttribute = dex.DefineAttribute[activecampaign.EcommerceOrderUpsertMetadata]("activecampaign-upserted-order")

// UpsertEcommerceOrderFlow writes a store order through the provider GraphQL API.
type UpsertEcommerceOrderFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewUpsertEcommerceOrderFlow binds the example's static connection.
func NewUpsertEcommerceOrderFlow(connection activecampaign.Connection) *UpsertEcommerceOrderFlow {
	return &UpsertEcommerceOrderFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*UpsertEcommerceOrderFlow) GetFlowType() string { return UpsertEcommerceOrderFlowType }

// GetPersistenceSchema stores the sanitized keyed order result.
func (*UpsertEcommerceOrderFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{upsertedOrderAttribute}}
}

// GetSteps registers the provider Step and completion path.
func (flow *UpsertEcommerceOrderFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUpsertEcommerceOrder{}),
		dex.DefineStep(activecampaign.NewUpsertEcommerceOrderStep(activecampaign.UpsertEcommerceOrderStepConfig[activecampaign.UpsertEcommerceOrderInput]{
			StepType: upsertEcommerceOrderStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Upsert a store order by connection and store order ID."},
			Connection:  flow.connection,
			MapToOperationInput: func(input activecampaign.UpsertEcommerceOrderInput) activecampaign.UpsertEcommerceOrderInput {
				return input
			},
			Upserted: sdkgo.GoTo(completeUpsertEcommerceOrder{}),
		})),
		dex.DefineStep(completeUpsertEcommerceOrder{}),
	}
}

// GetRPCs exposes the stored order result in Dex Web.
func (flow *UpsertEcommerceOrderFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-upserted-order value-type:json editable:false description:"Confirmed store order identity"
// GetDexSummary shows the confirmed order identity in Dex Web.
func (*UpsertEcommerceOrderFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	order, err := readUpsertedOrder(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-upserted-order": order}}, nil
}

// dex:field attribute-key:activecampaign-upserted-order value-type:json editable:false description:"Confirmed store order identity"
// GetDexDisplay shows the confirmed order identity in Dex Web.
func (*UpsertEcommerceOrderFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	order, err := readUpsertedOrder(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-upserted-order": order}}, nil
}

func readUpsertedOrder(ctx dex.Context) (activecampaign.EcommerceOrderUpsertMetadata, error) {
	order, err := upsertedOrderAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.EcommerceOrderUpsertMetadata{}, nil
	}
	return order, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the complete store order state to the GraphQL upsert."
type startUpsertEcommerceOrder struct{ dex.StepDefaults }

func (startUpsertEcommerceOrder) GetStepType() string { return "StartUpsertEcommerceOrder" }
func (startUpsertEcommerceOrder) WaitFor(dex.Context, activecampaign.UpsertEcommerceOrderInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUpsertEcommerceOrder) Execute(_ dex.Context, input activecampaign.UpsertEcommerceOrderInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.UpsertEcommerceOrderInput](upsertEcommerceOrderStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the confirmed store order identity."
type completeUpsertEcommerceOrder struct {
	dex.StepDefaultsNoWaitFor[activecampaign.UpsertEcommerceOrderResult]
}

func (completeUpsertEcommerceOrder) GetStepType() string { return "CompleteUpsertEcommerceOrder" }
func (completeUpsertEcommerceOrder) Execute(ctx dex.Context, result activecampaign.UpsertEcommerceOrderResult) (*dex.StepDecision, error) {
	if err := upsertedOrderAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpsertEcommerceOrderFlow)(nil)
