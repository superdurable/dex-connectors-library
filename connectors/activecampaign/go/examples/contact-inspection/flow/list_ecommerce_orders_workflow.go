// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ListEcommerceOrdersFlowType is the stable example Flow identity for order inspection.
const ListEcommerceOrdersFlowType = "ActiveCampaignEcommerceOrderInspection"

const listEcommerceOrdersStepType = "CallListEcommerceOrders"

var ordersPageAttribute = dex.DefineAttribute[activecampaign.EcommerceOrdersPage]("activecampaign-orders-page")

// ListEcommerceOrdersFlow reads one ecommerce order page and completes with typed metadata.
type ListEcommerceOrdersFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewListEcommerceOrdersFlow binds the provider connection at registration time.
func NewListEcommerceOrdersFlow(connection activecampaign.Connection) *ListEcommerceOrdersFlow {
	return &ListEcommerceOrdersFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*ListEcommerceOrdersFlow) GetFlowType() string { return ListEcommerceOrdersFlowType }

// GetPersistenceSchema registers the order page displayed in Dex Web.
func (*ListEcommerceOrdersFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{ordersPageAttribute}}
}

// GetSteps registers the order Query and completion path.
func (flow *ListEcommerceOrdersFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startListEcommerceOrders{}),
		dex.DefineStep(activecampaign.NewListEcommerceOrdersStep(activecampaign.ListEcommerceOrdersStepConfig[activecampaign.ListEcommerceOrdersInput]{
			StepType: listEcommerceOrdersStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Read one page of ecommerce orders and order totals."},
			Connection:  flow.connection,
			MapToOperationInput: func(input activecampaign.ListEcommerceOrdersInput) activecampaign.ListEcommerceOrdersInput {
				return input
			},
			Listed: sdkgo.GoTo(completeListEcommerceOrders{}),
		})),
		dex.DefineStep(completeListEcommerceOrders{}),
	}
}

// GetRPCs exposes read-only order summaries in Dex Web.
func (flow *ListEcommerceOrdersFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-orders-page value-type:json editable:false description:"Ecommerce order summaries"
// GetDexSummary shows the last order page in Dex Web.
func (*ListEcommerceOrdersFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readEcommerceOrdersPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-orders-page": page}}, nil
}

// dex:field attribute-key:activecampaign-orders-page value-type:json editable:false description:"Ecommerce order summaries"
// GetDexDisplay shows the last order page in Dex Web.
func (*ListEcommerceOrdersFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readEcommerceOrdersPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-orders-page": page}}, nil
}

func readEcommerceOrdersPage(ctx dex.Context) (activecampaign.EcommerceOrdersPage, error) {
	page, err := ordersPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.EcommerceOrdersPage{}, nil
	}
	return page, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass pagination to the order Query."
type startListEcommerceOrders struct{ dex.StepDefaults }

func (startListEcommerceOrders) GetStepType() string { return "StartListEcommerceOrders" }
func (startListEcommerceOrders) WaitFor(dex.Context, activecampaign.ListEcommerceOrdersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startListEcommerceOrders) Execute(_ dex.Context, input activecampaign.ListEcommerceOrdersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.ListEcommerceOrdersInput](listEcommerceOrdersStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with sanitized order metadata."
type completeListEcommerceOrders struct {
	dex.StepDefaultsNoWaitFor[activecampaign.ListEcommerceOrdersResult]
}

func (completeListEcommerceOrders) GetStepType() string { return "CompleteListEcommerceOrders" }
func (completeListEcommerceOrders) Execute(ctx dex.Context, result activecampaign.ListEcommerceOrdersResult) (*dex.StepDecision, error) {
	if err := ordersPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ListEcommerceOrdersFlow)(nil)
