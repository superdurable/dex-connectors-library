// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactioninspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/paddle/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// DiscountFlowType is the stable Paddle discount listing Flow identity.
const DiscountFlowType = "PaddleDiscountListing"

const discountStepType = "CallListDiscounts"

var discountPageAttribute = dex.DefineAttribute[paddle.DiscountPage]("paddle-discount-page")

// DiscountFlow lists one page of Paddle discounts.
type DiscountFlow struct {
	dex.FlowDefaults
	connection paddle.Connection
}

// NewDiscountFlow binds the Paddle connection for listing discounts.
func NewDiscountFlow(connection paddle.Connection) *DiscountFlow {
	return &DiscountFlow{connection: connection}
}

func (*DiscountFlow) GetFlowType() string { return DiscountFlowType }

func (*DiscountFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{discountPageAttribute}}
}

func (flow *DiscountFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDiscountList{}),
		dex.DefineStep(paddle.NewListDiscountsStep(paddle.ListDiscountsStepConfig[paddle.ListDiscountsInput]{
			StepType: discountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Paddle", Explanation: "List one page of Paddle discounts."},
			Connection:  flow.connection, MapToOperationInput: mapDiscountListInput,
			Listed: sdkgo.GoTo(completeDiscountList{}),
		})),
		dex.DefineStep(completeDiscountList{}),
	}
}

func (flow *DiscountFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:paddle-discount-page value-type:json editable:false description:"Discount page"
func (*DiscountFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readDiscountPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-discount-page": page}}, nil
}

// dex:field attribute-key:paddle-discount-page value-type:json editable:false description:"Discount page"
func (*DiscountFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readDiscountPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-discount-page": page}}, nil
}

func readDiscountPage(ctx dex.Context) (paddle.DiscountPage, error) {
	page, err := discountPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return paddle.DiscountPage{}, nil
	}
	return page, err
}

func mapDiscountListInput(input paddle.ListDiscountsInput) paddle.ListDiscountsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested discount page and cursor to Paddle."
type startDiscountList struct{ dex.StepDefaults }

func (startDiscountList) GetStepType() string { return "StartListDiscounts" }
func (startDiscountList) WaitFor(dex.Context, paddle.ListDiscountsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startDiscountList) Execute(_ dex.Context, input paddle.ListDiscountsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[paddle.ListDiscountsInput](discountStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the returned discount page."
type completeDiscountList struct {
	dex.StepDefaultsNoWaitFor[paddle.ListDiscountsResult]
}

func (completeDiscountList) GetStepType() string { return "CompleteListDiscounts" }
func (completeDiscountList) Execute(ctx dex.Context, result paddle.ListDiscountsResult) (*dex.StepDecision, error) {
	if err := discountPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*DiscountFlow)(nil)
