// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package customerfields runs OKKI listCustomerFields as one Dex Flow.
package customerfields

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/okki/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "OKKICustomerFields"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "okki"
	operationStepType = "CallListCustomerFields"
)

var fieldsAttribute = dex.DefineAttribute[okki.CustomerFields]("okki-customer-fields")

// Flow runs ListCustomerFields once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection okki.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection okki.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{fieldsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(okki.NewListCustomerFieldsStep(okki.ListCustomerFieldsStepConfig[okki.ListCustomerFieldsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "OKKI", Explanation: "Read the authorized OKKI customer field definitions."},
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

// dex:field attribute-key:okki-customer-fields value-type:json editable:false description:"OKKI customer field definitions information"
// GetDexSummary shows the last OKKI customer field definitions information in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readFields(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"okki-customer-fields": outcome}}, nil
}

// dex:field attribute-key:okki-customer-fields value-type:json editable:false description:"OKKI customer field definitions information"
// GetDexDisplay shows the last OKKI customer field definitions information in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readFields(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"okki-customer-fields": outcome}}, nil
}

func readFields(ctx dex.Context) (okki.CustomerFields, error) {
	outcome, err := fieldsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return okki.CustomerFields{}, nil
	}
	return outcome, err
}

// MapToOperationInput passes the typed start input for the provider call.
func MapToOperationInput(input okki.ListCustomerFieldsInput) okki.ListCustomerFieldsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListCustomerFields" }
func (startRequest) WaitFor(dex.Context, okki.ListCustomerFieldsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input okki.ListCustomerFieldsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[okki.ListCustomerFieldsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[okki.ListCustomerFieldsResult]
}

func (completeResult) GetStepType() string { return "CompleteListCustomerFields" }
func (completeResult) Execute(ctx dex.Context, result okki.ListCustomerFieldsResult) (*dex.StepDecision, error) {
	if err := fieldsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
