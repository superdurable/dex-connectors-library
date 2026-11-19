// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package employee

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/gusto/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// EmployeeListFlowType is the stable employee-list Flow identity.
const EmployeeListFlowType = "GustoEmployeeList"

var employeeListAttribute = dex.DefineAttribute[gusto.EmployeesPage]("gusto-employee-list")

// EmployeeListFlow reads one bounded page of employees.
type EmployeeListFlow struct {
	dex.FlowDefaults
	connection gusto.Connection
}

// NewEmployeeListFlow binds the provider connection at registration time.
func NewEmployeeListFlow(connection gusto.Connection) *EmployeeListFlow {
	return &EmployeeListFlow{connection: connection}
}

func (*EmployeeListFlow) GetFlowType() string { return EmployeeListFlowType }

func (*EmployeeListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{employeeListAttribute}}
}

func (flow *EmployeeListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startEmployeeList{}),
		dex.DefineStep(gusto.NewListEmployeesStep(gusto.ListEmployeesStepConfig[gusto.ListEmployeesInput]{
			StepType: "CallListEmployees", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Gusto", Explanation: "Read one page of authorized company employees."},
			Connection:          flow.connection,
			MapToOperationInput: func(input gusto.ListEmployeesInput) gusto.ListEmployeesInput { return input },
			Listed:              sdkgo.GoTo(completeEmployeeList{}),
		})),
		dex.DefineStep(completeEmployeeList{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *EmployeeListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:gusto-employee-list value-type:json editable:false description:"Employee page"
// GetDexSummary shows the last employee page in Dex Web.
func (*EmployeeListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readEmployeeList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gusto-employee-list": page}}, nil
}

// dex:field attribute-key:gusto-employee-list value-type:json editable:false description:"Employee page"
// GetDexDisplay shows the last employee page in Dex Web.
func (*EmployeeListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readEmployeeList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gusto-employee-list": page}}, nil
}

func readEmployeeList(ctx dex.Context) (gusto.EmployeesPage, error) {
	page, err := employeeListAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return gusto.EmployeesPage{}, nil
	}
	return page, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the company and page settings to the provider Step."
type startEmployeeList struct{ dex.StepDefaults }

func (startEmployeeList) GetStepType() string { return "StartEmployeeList" }
func (startEmployeeList) WaitFor(dex.Context, gusto.ListEmployeesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startEmployeeList) Execute(_ dex.Context, input gusto.ListEmployeesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[gusto.ListEmployeesInput]("CallListEmployees"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the selected employee page."
type completeEmployeeList struct {
	dex.StepDefaultsNoWaitFor[gusto.ListEmployeesResult]
}

func (completeEmployeeList) GetStepType() string { return "CompleteEmployeeList" }
func (completeEmployeeList) Execute(ctx dex.Context, result gusto.ListEmployeesResult) (*dex.StepDecision, error) {
	if err := employeeListAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*EmployeeListFlow)(nil)
