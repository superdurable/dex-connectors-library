// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package applications runs Tingyun listApplications as one Dex Flow.
package applications

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/tingyun/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TingyunApplications"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "tingyun"
	operationStepType = "CallListApplications"
)

var applicationsOutcomeAttribute = dex.DefineAttribute[tingyun.ListApplicationsOutput]("tingyun-list-applications-outcome")

// Flow runs ListApplications once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection tingyun.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection tingyun.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{applicationsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(tingyun.NewListApplicationsStep(tingyun.ListApplicationsStepConfig[tingyun.ListApplicationsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Tingyun", Explanation: "Call the provider's listApplications operation."},
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

// dex:field attribute-key:tingyun-list-applications-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListApplicationsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tingyun-list-applications-outcome": outcome}}, nil
}

// dex:field attribute-key:tingyun-list-applications-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListApplicationsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tingyun-list-applications-outcome": outcome}}, nil
}

func readListApplicationsOutcome(ctx dex.Context) (tingyun.ListApplicationsOutput, error) {
	outcome, err := applicationsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tingyun.ListApplicationsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input tingyun.ListApplicationsInput) tingyun.ListApplicationsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListApplications" }
func (startRequest) WaitFor(dex.Context, tingyun.ListApplicationsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input tingyun.ListApplicationsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tingyun.ListApplicationsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[tingyun.ListApplicationsResult]
}

func (completeResult) GetStepType() string { return "CompleteListApplications" }
func (completeResult) Execute(ctx dex.Context, result tingyun.ListApplicationsResult) (*dex.StepDecision, error) {
	if err := applicationsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
