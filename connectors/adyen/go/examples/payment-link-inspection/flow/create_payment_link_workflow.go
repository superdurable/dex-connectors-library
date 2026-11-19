// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paymentlinkinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adyen/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createPaymentLinkStepType = "CallCreatePaymentLink"

var createdPaymentLinkAttribute = dex.DefineAttribute[adyen.CreatedPaymentLink]("adyen-created-payment-link")

// CreatePaymentLinkFlow creates one shopper-facing payment link.
type CreatePaymentLinkFlow struct {
	dex.FlowDefaults
	connection adyen.Connection
}

// NewCreatePaymentLinkFlow binds the static Adyen connection.
func NewCreatePaymentLinkFlow(connection adyen.Connection) *CreatePaymentLinkFlow {
	return &CreatePaymentLinkFlow{connection: connection}
}

// GetFlowType identifies the creation example.
func (*CreatePaymentLinkFlow) GetFlowType() string { return "AdyenCreatePaymentLink" }

// GetPersistenceSchema registers the created link's public fields.
func (*CreatePaymentLinkFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdPaymentLinkAttribute}}
}

// GetSteps registers the idempotent creation and completion boundaries.
func (flow *CreatePaymentLinkFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPaymentLinkCreation{}),
		dex.DefineStep(adyen.NewCreatePaymentLinkStep(adyen.CreatePaymentLinkStepConfig[adyen.CreatePaymentLinkInput]{
			StepType: createPaymentLinkStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Adyen", Explanation: "Create an idempotent payment link."},
			Connection:  flow.connection, MapToOperationInput: MapToCreatePaymentLinkInput,
			Created: sdkgo.GoTo(completePaymentLinkCreation{}),
		})),
		dex.DefineStep(completePaymentLinkCreation{}),
	}
}

// GetRPCs exposes read-only Dex Web views.
func (flow *CreatePaymentLinkFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:adyen-created-payment-link value-type:json editable:false description:"Created payment link"
// GetDexSummary shows the created link in Dex Web.
func (*CreatePaymentLinkFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCreatedPaymentLink(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-created-payment-link": result}}, nil
}

// dex:field attribute-key:adyen-created-payment-link value-type:json editable:false description:"Created payment link"
// GetDexDisplay shows the created link in Dex Web.
func (*CreatePaymentLinkFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCreatedPaymentLink(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-created-payment-link": result}}, nil
}

func readCreatedPaymentLink(ctx dex.Context) (adyen.CreatedPaymentLink, error) {
	result, err := createdPaymentLinkAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return adyen.CreatedPaymentLink{}, nil
	}
	return result, err
}

// MapToCreatePaymentLinkInput preserves the typed start input.
func MapToCreatePaymentLinkInput(input adyen.CreatePaymentLinkInput) adyen.CreatePaymentLinkInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the merchant, reference, and amount to Adyen."
type startPaymentLinkCreation struct{ dex.StepDefaults }

func (startPaymentLinkCreation) GetStepType() string { return "StartCreatePaymentLink" }
func (startPaymentLinkCreation) WaitFor(dex.Context, adyen.CreatePaymentLinkInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPaymentLinkCreation) Execute(_ dex.Context, input adyen.CreatePaymentLinkInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[adyen.CreatePaymentLinkInput](createPaymentLinkStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the shopper-facing link."
type completePaymentLinkCreation struct {
	dex.StepDefaultsNoWaitFor[adyen.CreatePaymentLinkResult]
}

func (completePaymentLinkCreation) GetStepType() string { return "CompleteCreatePaymentLink" }
func (completePaymentLinkCreation) Execute(ctx dex.Context, result adyen.CreatePaymentLinkResult) (*dex.StepDecision, error) {
	if err := createdPaymentLinkAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreatePaymentLinkFlow)(nil)
