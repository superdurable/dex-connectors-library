// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package paymentlinkinspection runs Adyen getPaymentLink as one Dex Flow.
package paymentlinkinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adyen/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AdyenPaymentLinkInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "adyen"
	operationStepType = "CallGetPaymentLink"
)

var paymentLinkAttribute = dex.DefineAttribute[adyen.PaymentLinkStatus]("adyen-payment-link")

// Flow runs GetPaymentLink once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection adyen.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection adyen.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{paymentLinkAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(adyen.NewGetPaymentLinkStep(adyen.GetPaymentLinkStepConfig[adyen.GetPaymentLinkInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Adyen", Explanation: "Read a payment link from Adyen."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:adyen-payment-link value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPaymentLink(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-payment-link": outcome}}, nil
}

// dex:field attribute-key:adyen-payment-link value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPaymentLink(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-payment-link": outcome}}, nil
}

func readPaymentLink(ctx dex.Context) (adyen.PaymentLinkStatus, error) {
	outcome, err := paymentLinkAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return adyen.PaymentLinkStatus{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input adyen.GetPaymentLinkInput) adyen.GetPaymentLinkInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetPaymentLink" }
func (startRequest) WaitFor(dex.Context, adyen.GetPaymentLinkInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input adyen.GetPaymentLinkInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[adyen.GetPaymentLinkInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[adyen.GetPaymentLinkResult]
}

func (completeResult) GetStepType() string { return "CompleteGetPaymentLink" }
func (completeResult) Execute(ctx dex.Context, result adyen.GetPaymentLinkResult) (*dex.StepDecision, error) {
	if err := paymentLinkAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
