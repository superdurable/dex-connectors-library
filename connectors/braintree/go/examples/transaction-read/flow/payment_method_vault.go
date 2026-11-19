// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactionread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/braintree/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// PaymentMethodVaultFlowType identifies the payment method vault example.
const PaymentMethodVaultFlowType = "BraintreePaymentMethodVault"

const paymentMethodVaultOperationStepType = "CallVaultPaymentMethod"

var vaultedPaymentMethodAttribute = dex.DefineAttribute[braintree.PaymentMethodProfile]("braintree-payment-method-vaulted")

// PaymentMethodVaultFlow vaults one Braintree payment method through GraphQL.
type PaymentMethodVaultFlow struct {
	dex.FlowDefaults
	connection braintree.Connection
}

// NewPaymentMethodVaultFlow binds the provider connection at registration time.
func NewPaymentMethodVaultFlow(connection braintree.Connection) *PaymentMethodVaultFlow {
	return &PaymentMethodVaultFlow{connection: connection}
}

func (*PaymentMethodVaultFlow) GetFlowType() string { return PaymentMethodVaultFlowType }

func (*PaymentMethodVaultFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{vaultedPaymentMethodAttribute}}
}

func (flow *PaymentMethodVaultFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPaymentMethodVaultRequest{}),
		dex.DefineStep(braintree.NewVaultPaymentMethodStep(braintree.VaultPaymentMethodStepConfig[braintree.VaultPaymentMethodInput]{
			StepType: paymentMethodVaultOperationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braintree", Explanation: "Vault one single-use payment method for repeat use."},
			Connection:          flow.connection,
			MapToOperationInput: func(input braintree.VaultPaymentMethodInput) braintree.VaultPaymentMethodInput { return input },
			Vaulted:             sdkgo.GoTo(completePaymentMethodVaultResult{}),
		})),
		dex.DefineStep(completePaymentMethodVaultResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *PaymentMethodVaultFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:braintree-payment-method-vaulted value-type:json editable:false description:"Provider result"
// GetDexSummary shows the vaulted payment method.
func (*PaymentMethodVaultFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	paymentMethod, err := readVaultedPaymentMethod(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-payment-method-vaulted": paymentMethod}}, nil
}

// dex:field attribute-key:braintree-payment-method-vaulted value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the vaulted payment method.
func (*PaymentMethodVaultFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	paymentMethod, err := readVaultedPaymentMethod(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-payment-method-vaulted": paymentMethod}}, nil
}

func readVaultedPaymentMethod(ctx dex.Context) (braintree.PaymentMethodProfile, error) {
	paymentMethod, err := vaultedPaymentMethodAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return braintree.PaymentMethodProfile{}, nil
	}
	return paymentMethod, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the single-use payment method ID to the provider Step."
type startPaymentMethodVaultRequest struct{ dex.StepDefaults }

func (startPaymentMethodVaultRequest) GetStepType() string { return "StartVaultPaymentMethod" }
func (startPaymentMethodVaultRequest) WaitFor(dex.Context, braintree.VaultPaymentMethodInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPaymentMethodVaultRequest) Execute(_ dex.Context, input braintree.VaultPaymentMethodInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[braintree.VaultPaymentMethodInput](paymentMethodVaultOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the payment method result."
type completePaymentMethodVaultResult struct {
	dex.StepDefaultsNoWaitFor[braintree.VaultPaymentMethodResult]
}

func (completePaymentMethodVaultResult) GetStepType() string { return "CompleteVaultPaymentMethod" }
func (completePaymentMethodVaultResult) Execute(ctx dex.Context, result braintree.VaultPaymentMethodResult) (*dex.StepDecision, error) {
	if err := vaultedPaymentMethodAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PaymentMethodVaultFlow)(nil)
