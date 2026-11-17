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
	// customerUpsertFlowType is the stable example Flow identity.
	customerUpsertFlowType = "CustomerioCustomerUpsert"
	customerUpsertStepType = "CallUpsertCustomer"
)

var customerUpsertAttribute = dex.DefineAttribute[customerio.CustomerWriteOutput]("customerio-customerupsert")

// CustomerUpsertFlow runs UpsertCustomer once and completes with its typed output.
type CustomerUpsertFlow struct {
	dex.FlowDefaults
	connection customerio.Connection
}

// NewCustomerUpsertFlow binds the provider connection at registration time.
func NewCustomerUpsertFlow(connection customerio.Connection) *CustomerUpsertFlow {
	return &CustomerUpsertFlow{connection: connection}
}

func (*CustomerUpsertFlow) GetFlowType() string { return customerUpsertFlowType }

func (*CustomerUpsertFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customerUpsertAttribute}}
}

func (flow *CustomerUpsertFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCustomerUpsertFlow{}),
		dex.DefineStep(customerio.NewUpsertCustomerStep(customerio.UpsertCustomerStepConfig[customerio.UpsertCustomerInput]{
			StepType: customerUpsertStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Customer.io", Explanation: "Add or update customer attributes in Customer.io."},
			Connection:          flow.connection,
			MapToOperationInput: mapCustomerUpsertFlowInput,
			Upserted:            sdkgo.GoTo(completeCustomerUpsertFlow{}),
		})),
		dex.DefineStep(completeCustomerUpsertFlow{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *CustomerUpsertFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:customerio-customerupsert value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*CustomerUpsertFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCustomerUpsert(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-customerupsert": outcome}}, nil
}

// dex:field attribute-key:customerio-customerupsert value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*CustomerUpsertFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCustomerUpsert(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"customerio-customerupsert": outcome}}, nil
}

func readCustomerUpsert(ctx dex.Context) (customerio.CustomerWriteOutput, error) {
	outcome, err := customerUpsertAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return customerio.CustomerWriteOutput{}, nil
	}
	return outcome, err
}

// mapCustomerUpsertFlowInput preserves the validated typed start input for the provider call.
func mapCustomerUpsertFlowInput(input customerio.UpsertCustomerInput) customerio.UpsertCustomerInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startCustomerUpsertFlow struct{ dex.StepDefaults }

func (startCustomerUpsertFlow) GetStepType() string { return "StartUpsertCustomer" }
func (startCustomerUpsertFlow) WaitFor(dex.Context, customerio.UpsertCustomerInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCustomerUpsertFlow) Execute(_ dex.Context, input customerio.UpsertCustomerInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[customerio.UpsertCustomerInput](customerUpsertStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeCustomerUpsertFlow struct {
	dex.StepDefaultsNoWaitFor[customerio.UpsertCustomerResult]
}

func (completeCustomerUpsertFlow) GetStepType() string { return "CompleteUpsertCustomer" }
func (completeCustomerUpsertFlow) Execute(ctx dex.Context, result customerio.UpsertCustomerResult) (*dex.StepDecision, error) {
	if err := customerUpsertAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CustomerUpsertFlow)(nil)
