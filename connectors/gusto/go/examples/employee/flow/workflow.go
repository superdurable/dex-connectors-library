// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package gusto runs Gusto getEmployee as one Dex Flow.
package employee

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/gusto/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GustoEmployeeLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "gusto"
	operationStepType = "CallGetEmployee"
)

var employeeAttribute = dex.DefineAttribute[gusto.Employee]("gusto-employee")

// Flow runs GetEmployee once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection gusto.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection gusto.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{employeeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(gusto.NewGetEmployeeStep(gusto.GetEmployeeStepConfig[gusto.GetEmployeeInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Gusto", Explanation: "Read the selected employee using the authorized Gusto company."},
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

// dex:field attribute-key:gusto-employee value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readEmployee(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gusto-employee": outcome}}, nil
}

// dex:field attribute-key:gusto-employee value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readEmployee(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gusto-employee": outcome}}, nil
}

func readEmployee(ctx dex.Context) (gusto.Employee, error) {
	outcome, err := employeeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return gusto.Employee{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input gusto.GetEmployeeInput) gusto.GetEmployeeInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetEmployee" }
func (startRequest) WaitFor(dex.Context, gusto.GetEmployeeInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input gusto.GetEmployeeInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[gusto.GetEmployeeInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[gusto.GetEmployeeResult]
}

func (completeResult) GetStepType() string { return "CompleteGetEmployee" }
func (completeResult) Execute(ctx dex.Context, result gusto.GetEmployeeResult) (*dex.StepDecision, error) {
	if err := employeeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
