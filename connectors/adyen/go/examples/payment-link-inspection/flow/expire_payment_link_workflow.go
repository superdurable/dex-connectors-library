// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paymentlinkinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adyen/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const expirePaymentLinkStepType = "CallExpirePaymentLink"

var expiredPaymentLinkAttribute = dex.DefineAttribute[adyen.PaymentLinkStatus]("adyen-expired-payment-link")

// ExpirePaymentLinkFlow demonstrates an absolute payment link status update.
type ExpirePaymentLinkFlow struct {
	dex.FlowDefaults
	connection adyen.Connection
}

// NewExpirePaymentLinkFlow binds the static Adyen connection.
func NewExpirePaymentLinkFlow(connection adyen.Connection) *ExpirePaymentLinkFlow {
	return &ExpirePaymentLinkFlow{connection: connection}
}

// GetFlowType identifies this example Flow.
func (*ExpirePaymentLinkFlow) GetFlowType() string { return "AdyenExpirePaymentLink" }

// GetPersistenceSchema registers the status shown in Dex Web.
func (*ExpirePaymentLinkFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{expiredPaymentLinkAttribute}}
}

// GetSteps registers the provider mutation and completion.
func (flow *ExpirePaymentLinkFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPaymentLinkExpiry{}),
		dex.DefineStep(adyen.NewExpirePaymentLinkStep(adyen.ExpirePaymentLinkStepConfig[adyen.ExpirePaymentLinkInput]{
			StepType: expirePaymentLinkStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Adyen", Explanation: "Force a payment link to expire."},
			Connection:  flow.connection, MapToOperationInput: MapToExpirePaymentLinkInput,
			Expired: sdkgo.GoTo(completePaymentLinkExpiry{}),
		})),
		dex.DefineStep(completePaymentLinkExpiry{}),
	}
}

// GetRPCs exposes read-only Dex Web views.
func (flow *ExpirePaymentLinkFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:adyen-expired-payment-link value-type:json editable:false description:"Expired payment link"
// GetDexSummary shows the result in Dex Web.
func (*ExpirePaymentLinkFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readExpiredPaymentLink(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-expired-payment-link": result}}, nil
}

// dex:field attribute-key:adyen-expired-payment-link value-type:json editable:false description:"Expired payment link"
// GetDexDisplay shows the result in Dex Web.
func (*ExpirePaymentLinkFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readExpiredPaymentLink(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-expired-payment-link": result}}, nil
}

func readExpiredPaymentLink(ctx dex.Context) (adyen.PaymentLinkStatus, error) {
	result, err := expiredPaymentLinkAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return adyen.PaymentLinkStatus{}, nil
	}
	return result, err
}

// MapToExpirePaymentLinkInput preserves the typed start input.
func MapToExpirePaymentLinkInput(input adyen.ExpirePaymentLinkInput) adyen.ExpirePaymentLinkInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected link ID to Adyen."
type startPaymentLinkExpiry struct{ dex.StepDefaults }

func (startPaymentLinkExpiry) GetStepType() string { return "StartExpirePaymentLink" }
func (startPaymentLinkExpiry) WaitFor(dex.Context, adyen.ExpirePaymentLinkInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPaymentLinkExpiry) Execute(_ dex.Context, input adyen.ExpirePaymentLinkInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[adyen.ExpirePaymentLinkInput](expirePaymentLinkStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the expired status."
type completePaymentLinkExpiry struct {
	dex.StepDefaultsNoWaitFor[adyen.ExpirePaymentLinkResult]
}

func (completePaymentLinkExpiry) GetStepType() string { return "CompleteExpirePaymentLink" }
func (completePaymentLinkExpiry) Execute(ctx dex.Context, result adyen.ExpirePaymentLinkResult) (*dex.StepDecision, error) {
	if err := expiredPaymentLinkAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ExpirePaymentLinkFlow)(nil)
