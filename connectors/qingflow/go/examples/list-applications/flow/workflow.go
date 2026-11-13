// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package applicationslist runs Qingflow listApplications as one Dex Flow.
package applicationslist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/qingflow/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "QingflowApplications"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "qingflow"
	operationStepType = "CallListApplications"
)

var applicationsOutcomeAttribute = dex.DefineAttribute[qingflow.ListApplicationsOutput]("qingflow-list-applications-outcome")

// Flow runs ListApplications once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection qingflow.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection qingflow.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{applicationsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(qingflow.NewListApplicationsStep(qingflow.ListApplicationsStepConfig[qingflow.ListApplicationsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Qingflow", Explanation: "Call the provider's listApplications operation."},
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

// dex:field attribute-key:qingflow-list-applications-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListApplicationsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"qingflow-list-applications-outcome": outcome}}, nil
}

// dex:field attribute-key:qingflow-list-applications-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListApplicationsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"qingflow-list-applications-outcome": outcome}}, nil
}

func readListApplicationsOutcome(ctx dex.Context) (qingflow.ListApplicationsOutput, error) {
	outcome, err := applicationsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return qingflow.ListApplicationsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input qingflow.ListApplicationsInput) qingflow.ListApplicationsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListApplications" }
func (startRequest) WaitFor(dex.Context, qingflow.ListApplicationsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input qingflow.ListApplicationsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[qingflow.ListApplicationsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[qingflow.ListApplicationsResult]
}

func (completeResult) GetStepType() string { return "CompleteListApplications" }
func (completeResult) Execute(ctx dex.Context, result qingflow.ListApplicationsResult) (*dex.StepDecision, error) {
	if err := applicationsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
