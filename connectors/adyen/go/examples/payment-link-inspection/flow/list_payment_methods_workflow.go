// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paymentlinkinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adyen/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listPaymentMethodsStepType = "CallListPaymentMethods"

var paymentMethodsAttribute = dex.DefineAttribute[adyen.PaymentMethodPage]("adyen-payment-methods")

// ListPaymentMethodsFlow lists method codes without stored shopper methods.
type ListPaymentMethodsFlow struct {
	dex.FlowDefaults
	connection adyen.Connection
}

// NewListPaymentMethodsFlow binds the static Adyen connection.
func NewListPaymentMethodsFlow(connection adyen.Connection) *ListPaymentMethodsFlow {
	return &ListPaymentMethodsFlow{connection: connection}
}

// GetFlowType identifies this example Flow.
func (*ListPaymentMethodsFlow) GetFlowType() string { return "AdyenListPaymentMethods" }

// GetPersistenceSchema registers the filtered method page.
func (*ListPaymentMethodsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{paymentMethodsAttribute}}
}

// GetSteps registers a Query and completion.
func (flow *ListPaymentMethodsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPaymentMethodList{}),
		dex.DefineStep(adyen.NewListPaymentMethodsStep(adyen.ListPaymentMethodsStepConfig[adyen.ListPaymentMethodsInput]{
			StepType: listPaymentMethodsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Adyen", Explanation: "List available method names and codes."},
			Connection:  flow.connection, MapToOperationInput: MapToListPaymentMethodsInput,
			Listed: sdkgo.GoTo(completePaymentMethodList{}),
		})),
		dex.DefineStep(completePaymentMethodList{}),
	}
}

// GetRPCs exposes read-only Dex Web views.
func (flow *ListPaymentMethodsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:adyen-payment-methods value-type:json editable:false description:"Available payment methods"
// GetDexSummary shows method codes in Dex Web.
func (*ListPaymentMethodsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readPaymentMethods(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-payment-methods": result}}, nil
}

// dex:field attribute-key:adyen-payment-methods value-type:json editable:false description:"Available payment methods"
// GetDexDisplay shows method codes in Dex Web.
func (*ListPaymentMethodsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readPaymentMethods(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-payment-methods": result}}, nil
}

func readPaymentMethods(ctx dex.Context) (adyen.PaymentMethodPage, error) {
	result, err := paymentMethodsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return adyen.PaymentMethodPage{}, nil
	}
	return result, err
}

// MapToListPaymentMethodsInput preserves the typed start input.
func MapToListPaymentMethodsInput(input adyen.ListPaymentMethodsInput) adyen.ListPaymentMethodsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the merchant and proposed amount to Adyen."
type startPaymentMethodList struct{ dex.StepDefaults }

func (startPaymentMethodList) GetStepType() string { return "StartListPaymentMethods" }
func (startPaymentMethodList) WaitFor(dex.Context, adyen.ListPaymentMethodsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPaymentMethodList) Execute(_ dex.Context, input adyen.ListPaymentMethodsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[adyen.ListPaymentMethodsInput](listPaymentMethodsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with safe payment method names."
type completePaymentMethodList struct {
	dex.StepDefaultsNoWaitFor[adyen.ListPaymentMethodsResult]
}

func (completePaymentMethodList) GetStepType() string { return "CompleteListPaymentMethods" }
func (completePaymentMethodList) Execute(ctx dex.Context, result adyen.ListPaymentMethodsResult) (*dex.StepDecision, error) {
	if err := paymentMethodsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ListPaymentMethodsFlow)(nil)
