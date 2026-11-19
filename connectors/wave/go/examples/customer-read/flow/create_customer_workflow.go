// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerread

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/wave/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CreateCustomerFlowType identifies the customer creation example.
const CreateCustomerFlowType = "WaveCreateCustomer"

var createdCustomerAttribute = dex.DefineAttribute[wave.CustomerProfile]("wave-created-customer")

// CreateCustomerFlow creates a named customer through the static Wave connection.
type CreateCustomerFlow struct {
	dex.FlowDefaults
	connection wave.Connection
}

// NewCreateCustomerFlow binds the Wave connection at registration time.
func NewCreateCustomerFlow(connection wave.Connection) *CreateCustomerFlow {
	return &CreateCustomerFlow{connection: connection}
}

// GetFlowType returns the stable creation Flow identity.
func (*CreateCustomerFlow) GetFlowType() string { return CreateCustomerFlowType }

// GetPersistenceSchema registers the created customer Attribute.
func (*CreateCustomerFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdCustomerAttribute}}
}

// GetSteps registers the start, provider, and completion Steps.
func (flow *CreateCustomerFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateCustomer{}),
		dex.DefineStep(wave.NewCreateCustomerStep(wave.CreateCustomerStepConfig[wave.CreateCustomerInput]{
			StepType: "CallCreateCustomer", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Wave", Explanation: "Create one named Wave customer."},
			MapToOperationInput: mapCreateCustomerInput, Created: sdkgo.GoTo(completeCreateCustomer{}),
		})),
		dex.DefineStep(completeCreateCustomer{}),
	}
}

// GetRPCs registers read-only Dex Web views.
func (flow *CreateCustomerFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:wave-created-customer value-type:json editable:false description:"Created customer"
// GetDexSummary returns the created customer when available.
func (*CreateCustomerFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedCustomer(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wave-created-customer": value}}, nil
}

// dex:field attribute-key:wave-created-customer value-type:json editable:false description:"Created customer"
// GetDexDisplay returns the created customer when available.
func (*CreateCustomerFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedCustomer(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wave-created-customer": value}}, nil
}

func readCreatedCustomer(ctx dex.Context) (wave.CustomerProfile, error) {
	value, err := createdCustomerAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wave.CustomerProfile{}, nil
	}
	return value, err
}
func mapCreateCustomerInput(input wave.CreateCustomerInput) wave.CreateCustomerInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed business and customer name to the provider Step."
type startCreateCustomer struct{ dex.StepDefaults }

func (startCreateCustomer) GetStepType() string { return "StartWaveCustomerCreation" }
func (startCreateCustomer) WaitFor(dex.Context, wave.CreateCustomerInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateCustomer) Execute(_ dex.Context, input wave.CreateCustomerInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wave.CreateCustomerInput]("CallCreateCustomer"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the created customer profile."
type completeCreateCustomer struct {
	dex.StepDefaultsNoWaitFor[wave.CreateCustomerResult]
}

func (completeCreateCustomer) GetStepType() string { return "CompleteWaveCustomerCreation" }
func (completeCreateCustomer) Execute(ctx dex.Context, result wave.CreateCustomerResult) (*dex.StepDecision, error) {
	if err := createdCustomerAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateCustomerFlow)(nil)
