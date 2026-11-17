// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/customerio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// customerDeletionFlowType is the stable example Flow identity.
	customerDeletionFlowType = "CustomerioCustomerDeletion"
	customerDeletionStepType = "CallDeleteCustomer"
)

var customerDeletionAttribute = dex.DefineAttribute[customerio.CustomerWriteOutput]("customerio-customerdeletion")

// CustomerDeletionFlow runs DeleteCustomer once and completes with its typed output.
type CustomerDeletionFlow struct {
	dex.FlowDefaults
	connection customerio.Connection
}

// NewCustomerDeletionFlow binds the provider connection at registration time.
func NewCustomerDeletionFlow(connection customerio.Connection) *CustomerDeletionFlow {
	return &CustomerDeletionFlow{connection: connection}
}

func (*CustomerDeletionFlow) GetFlowType() string { return customerDeletionFlowType }

func (*CustomerDeletionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customerDeletionAttribute}}
}

func (flow *CustomerDeletionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCustomerDeletionFlow{}),
		dex.DefineStep(customerio.NewDeleteCustomerStep(customerio.DeleteCustomerStepConfig[customerio.DeleteCustomerInput]{
			StepType: customerDeletionStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Customer.io", Explanation: "Delete a customer from Customer.io."},
			Connection:          flow.connection,
			MapToOperationInput: mapCustomerDeletionFlowInput,
			Deleted:             sdkgo.GoTo(completeCustomerDeletionFlow{}),
		})),
		dex.DefineStep(completeCustomerDeletionFlow{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *CustomerDeletionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:customerio-customerdeletion value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*CustomerDeletionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCustomerDeletion(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-customerdeletion": outcome}}, nil
}

// dex:field attribute-key:customerio-customerdeletion value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*CustomerDeletionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCustomerDeletion(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-customerdeletion": outcome}}, nil
}

func readCustomerDeletion(ctx dex.Context) (customerio.CustomerWriteOutput, error) {
	outcome, err := customerDeletionAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return customerio.CustomerWriteOutput{}, nil
	}
	return outcome, err
}

// mapCustomerDeletionFlowInput preserves the validated typed start input for the provider call.
func mapCustomerDeletionFlowInput(input customerio.DeleteCustomerInput) customerio.DeleteCustomerInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startCustomerDeletionFlow struct{ dex.StepDefaults }

func (startCustomerDeletionFlow) GetStepType() string { return "StartDeleteCustomer" }
func (startCustomerDeletionFlow) WaitFor(dex.Context, customerio.DeleteCustomerInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCustomerDeletionFlow) Execute(_ dex.Context, input customerio.DeleteCustomerInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[customerio.DeleteCustomerInput](customerDeletionStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeCustomerDeletionFlow struct {
	dex.StepDefaultsNoWaitFor[customerio.DeleteCustomerResult]
}

func (completeCustomerDeletionFlow) GetStepType() string { return "CompleteDeleteCustomer" }
func (completeCustomerDeletionFlow) Execute(ctx dex.Context, result customerio.DeleteCustomerResult) (*dex.StepDecision, error) {
	if err := customerDeletionAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CustomerDeletionFlow)(nil)
