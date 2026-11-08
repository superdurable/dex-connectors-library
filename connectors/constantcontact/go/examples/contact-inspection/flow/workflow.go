// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contactinspection runs Constant Contact getContact as one Dex Flow.
package contactinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/constantcontact/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ConstantContactContactInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "constantcontact"
	operationStepType = "CallGetContact"
)

var contactAttribute = dex.DefineAttribute[constantcontact.ContactMetadata]("constantcontact-contact")

// Flow runs GetContact once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection constantcontact.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection constantcontact.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(constantcontact.NewGetContactStep(constantcontact.GetContactStepConfig[constantcontact.GetContactInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Constant Contact", Explanation: "Read a contact from Constant Contact."},
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

// dex:field attribute-key:constantcontact-contact value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-contact": outcome}}, nil
}

// dex:field attribute-key:constantcontact-contact value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-contact": outcome}}, nil
}

func readContact(ctx dex.Context) (constantcontact.ContactMetadata, error) {
	outcome, err := contactAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return constantcontact.ContactMetadata{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input constantcontact.GetContactInput) constantcontact.GetContactInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetContact" }
func (startRequest) WaitFor(dex.Context, constantcontact.GetContactInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input constantcontact.GetContactInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[constantcontact.GetContactInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[constantcontact.GetContactResult]
}

func (completeResult) GetStepType() string { return "CompleteGetContact" }
func (completeResult) Execute(ctx dex.Context, result constantcontact.GetContactResult) (*dex.StepDecision, error) {
	if err := contactAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
