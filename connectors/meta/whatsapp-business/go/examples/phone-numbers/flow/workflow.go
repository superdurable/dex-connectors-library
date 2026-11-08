// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package phonenumbers runs WhatsApp Business listPhoneNumbers as one Dex Flow.
package phonenumbers

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/whatsapp-business/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WhatsAppBusinessPhoneNumbers"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "whatsapp-business"
	operationStepType = "CallListPhoneNumbers"
)

var phoneNumbersPageAttribute = dex.DefineAttribute[whatsappbusiness.PhoneNumbersPage]("whatsapp-business-phone-numbers-page")

// Flow runs ListPhoneNumbers once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection whatsappbusiness.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection whatsappbusiness.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{phoneNumbersPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(whatsappbusiness.NewListPhoneNumbersStep(whatsappbusiness.ListPhoneNumbersStepConfig[whatsappbusiness.ListPhoneNumbersInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "WhatsApp Business", Explanation: "Call the provider's listPhoneNumbers operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:whatsapp-business-phone-numbers-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPhoneNumbersPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"whatsapp-business-phone-numbers-page": outcome}}, nil
}

// dex:field attribute-key:whatsapp-business-phone-numbers-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPhoneNumbersPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"whatsapp-business-phone-numbers-page": outcome}}, nil
}

func readPhoneNumbersPage(ctx dex.Context) (whatsappbusiness.PhoneNumbersPage, error) {
	outcome, err := phoneNumbersPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return whatsappbusiness.PhoneNumbersPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input whatsappbusiness.ListPhoneNumbersInput) whatsappbusiness.ListPhoneNumbersInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListPhoneNumbers" }
func (startRequest) WaitFor(dex.Context, whatsappbusiness.ListPhoneNumbersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input whatsappbusiness.ListPhoneNumbersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[whatsappbusiness.ListPhoneNumbersInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[whatsappbusiness.ListPhoneNumbersResult]
}

func (completeResult) GetStepType() string { return "CompleteListPhoneNumbers" }
func (completeResult) Execute(ctx dex.Context, result whatsappbusiness.ListPhoneNumbersResult) (*dex.StepDecision, error) {
	if err := phoneNumbersPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
