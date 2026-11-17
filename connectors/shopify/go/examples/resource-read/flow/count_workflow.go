// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/shopify/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CountFlowType is the stable order-count Flow identity.
const CountFlowType = "ShopifyOrderCount"

// CountRequest selects the status to count; blank counts every status.
type CountRequest struct {
	// Status is an optional Shopify order status; blank counts every status.
	Status string `json:"status,omitempty"`
}

// CountOutcome is the counted number of orders.
type CountOutcome struct {
	// Count is the non-negative number of matching orders.
	Count int64 `json:"count"`
}

var countOutcomeAttribute = dex.DefineAttribute[CountOutcome]("shopify-order-count-outcome")

// CountFlow counts Shopify orders on the static connection.
type CountFlow struct {
	dex.FlowDefaults
	connection shopify.Connection
}

// NewCountFlow binds the connection at registration.
func NewCountFlow(connection shopify.Connection) *CountFlow {
	return &CountFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*CountFlow) GetFlowType() string { return CountFlowType }

// GetPersistenceSchema registers the count outcome Attribute.
func (*CountFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{countOutcomeAttribute}}
}

// GetSteps defines the count Query and completion Step.
func (flow *CountFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(shopify.NewCountOrdersStep(shopify.CountOrdersStepConfig[CountRequest]{
			StepType: "CountShopifyOrders", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "shopify", GroupLabel: "Shopify", Explanation: "Count orders in the requested status."},
			MapToOperationInput: func(request CountRequest) shopify.CountOrdersInput {
				return shopify.CountOrdersInput{Status: request.Status}
			},
			Counted: sdkgo.GoTo(completeCount{}),
		})),
		dex.DefineStep(completeCount{}),
	}
}

// GetRPCs exposes the summary and display RPCs.
func (flow *CountFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the persisted count for Dex Web.
// dex:field attribute-key:shopify-order-count-outcome value-type:json editable:false description:"Order count"
func (*CountFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := countOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shopify-order-count-outcome": outcome}}, nil
}

// GetDexDisplay returns the persisted count for Dex Web.
// dex:field attribute-key:shopify-order-count-outcome value-type:json editable:false description:"Order count"
func (*CountFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := countOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shopify-order-count-outcome": outcome}}, nil
}

// dex:group group-id:count-result group-label:"Order count result"
// dex:explanation text:"Persist the provider count and complete the Flow."
type completeCount struct {
	dex.StepDefaultsNoWaitFor[shopify.CountOrdersResult]
}

func (completeCount) GetStepType() string { return "CompleteShopifyOrderCount" }
func (completeCount) Execute(ctx dex.Context, result shopify.CountOrdersResult) (*dex.StepDecision, error) {
	outcome := CountOutcome{Count: result.Value.Count}
	if err := countOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*CountFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*CountFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*CountFlow)(nil).GetDexDisplay
