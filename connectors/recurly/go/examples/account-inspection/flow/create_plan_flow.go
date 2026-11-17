// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/recurly/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createPlanFlowType = "RecurlyCreatePlan"
const createPlanStepType = "CallCreatePlan"

var createdPlanAttribute = dex.DefineAttribute[recurly.PlanSummary]("recurly-created-plan")

// CreatePlanFlow creates one plan with a call-specific code.
type CreatePlanFlow struct {
	dex.FlowDefaults
	connection recurly.Connection
}

// NewCreatePlanFlow binds the static Recurly connection.
func NewCreatePlanFlow(connection recurly.Connection) *CreatePlanFlow {
	return &CreatePlanFlow{connection: connection}
}

func (*CreatePlanFlow) GetFlowType() string { return createPlanFlowType }

func (*CreatePlanFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdPlanAttribute}}
}

func (flow *CreatePlanFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreatePlan{}),
		dex.DefineStep(recurly.NewCreatePlanStep(recurly.CreatePlanStepConfig[recurly.CreatePlanInput]{
			StepType: createPlanStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Recurly", Explanation: "Create one plan with a call-specific code."},
			Connection:          flow.connection,
			MapToOperationInput: mapCreatePlanInput,
			Created:             sdkgo.GoTo(completeCreatePlan{}),
		})),
		dex.DefineStep(completeCreatePlan{}),
	}
}

func (flow *CreatePlanFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:recurly-created-plan value-type:json editable:false description:"Created plan"
// GetDexSummary returns the safe plan summary for Dex Web.
func (*CreatePlanFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedPlan(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-created-plan": value}}, nil
}

// dex:field attribute-key:recurly-created-plan value-type:json editable:false description:"Created plan"
// GetDexDisplay returns the safe plan summary for Dex Web.
func (*CreatePlanFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedPlan(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-created-plan": value}}, nil
}

func readCreatedPlan(ctx dex.Context) (recurly.PlanSummary, error) {
	value, err := createdPlanAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return recurly.PlanSummary{}, nil
	}
	return value, err
}

func mapCreatePlanInput(input recurly.CreatePlanInput) recurly.CreatePlanInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass amount and currency to the provider Step."
type startCreatePlan struct{ dex.StepDefaults }

func (startCreatePlan) GetStepType() string { return "StartCreatePlan" }
func (startCreatePlan) WaitFor(dex.Context, recurly.CreatePlanInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreatePlan) Execute(_ dex.Context, input recurly.CreatePlanInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[recurly.CreatePlanInput](createPlanStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after Recurly confirms the plan."
type completeCreatePlan struct {
	dex.StepDefaultsNoWaitFor[recurly.CreatePlanResult]
}

func (completeCreatePlan) GetStepType() string { return "CompleteCreatePlan" }
func (completeCreatePlan) Execute(ctx dex.Context, result recurly.CreatePlanResult) (*dex.StepDecision, error) {
	if err := createdPlanAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreatePlanFlow)(nil)
