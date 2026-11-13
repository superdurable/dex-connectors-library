// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package departmentcatalog runs Qince listDepartments as one Dex Flow.
package departmentcatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/qince/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "QinceDepartmentCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "qince"
	operationStepType = "CallListDepartments"
)

var departmentCatalogOutcomeAttribute = dex.DefineAttribute[qince.ListDepartmentsOutput]("qince-department-catalog-outcome")

// Flow runs ListDepartments once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection qince.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection qince.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{departmentCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(qince.NewListDepartmentsStep(qince.ListDepartmentsStepConfig[qince.ListDepartmentsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Qince", Explanation: "Read department IDs, codes, names, and active status."},
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

// dex:field attribute-key:qince-department-catalog-outcome value-type:json editable:false description:"Department catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDepartmentCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"qince-department-catalog-outcome": outcome}}, nil
}

// dex:field attribute-key:qince-department-catalog-outcome value-type:json editable:false description:"Department catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDepartmentCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"qince-department-catalog-outcome": outcome}}, nil
}

func readDepartmentCatalogOutcome(ctx dex.Context) (qince.ListDepartmentsOutput, error) {
	outcome, err := departmentCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return qince.ListDepartmentsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input qince.ListDepartmentsInput) qince.ListDepartmentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListDepartments" }
func (startRequest) WaitFor(dex.Context, qince.ListDepartmentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input qince.ListDepartmentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[qince.ListDepartmentsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[qince.ListDepartmentsResult]
}

func (completeResult) GetStepType() string { return "CompleteListDepartments" }
func (completeResult) Execute(ctx dex.Context, result qince.ListDepartmentsResult) (*dex.StepDecision, error) {
	if err := departmentCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
