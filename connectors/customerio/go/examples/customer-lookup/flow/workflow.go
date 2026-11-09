// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package customerlookup runs Customerio getCustomersByEmail as one Dex Flow.
package customerlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/customerio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "CustomerioCustomerLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "customerio"
	operationStepType = "CallGetCustomersByEmail"
)

var customersAttribute = dex.DefineAttribute[customerio.CustomerLookupPage]("customerio-customer-lookup")

// Flow runs GetCustomersByEmail once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection customerio.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection customerio.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customersAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(customerio.NewGetCustomersByEmailStep(customerio.GetCustomersByEmailStepConfig[customerio.GetCustomersByEmailInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Customer.io", Explanation: "Find customers by email in Customer.io."},
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

// dex:field attribute-key:customerio-customer-lookup value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCustomers(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-customer-lookup": outcome}}, nil
}

// dex:field attribute-key:customerio-customer-lookup value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCustomers(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-customer-lookup": outcome}}, nil
}

func readCustomers(ctx dex.Context) (customerio.CustomerLookupPage, error) {
	outcome, err := customersAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return customerio.CustomerLookupPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input customerio.GetCustomersByEmailInput) customerio.GetCustomersByEmailInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetCustomersByEmail" }
func (startRequest) WaitFor(dex.Context, customerio.GetCustomersByEmailInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input customerio.GetCustomersByEmailInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[customerio.GetCustomersByEmailInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[customerio.GetCustomersByEmailResult]
}

func (completeResult) GetStepType() string { return "CompleteGetCustomersByEmail" }
func (completeResult) Execute(ctx dex.Context, result customerio.GetCustomersByEmailResult) (*dex.StepDecision, error) {
	if err := customersAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
