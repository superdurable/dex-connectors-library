// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package deploymentlist runs EMQ listDeployments as one Dex Flow.
package deploymentlist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/emq/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "EMQDeploymentList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "emq"
	operationStepType = "CallListDeployments"
)

var deploymentListOutcomeAttribute = dex.DefineAttribute[emq.ListDeploymentsOutput]("emq-deployment-list-outcome")

// Flow runs ListDeployments once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection emq.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection emq.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{deploymentListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(emq.NewListDeploymentsStep(emq.ListDeploymentsStepConfig[emq.ListDeploymentsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "EMQ", Explanation: "Call the provider's listDeployments operation."},
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

// dex:field attribute-key:emq-deployment-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDeploymentListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"emq-deployment-list-outcome": outcome}}, nil
}

// dex:field attribute-key:emq-deployment-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDeploymentListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"emq-deployment-list-outcome": outcome}}, nil
}

func readDeploymentListOutcome(ctx dex.Context) (emq.ListDeploymentsOutput, error) {
	outcome, err := deploymentListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return emq.ListDeploymentsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input emq.ListDeploymentsInput) emq.ListDeploymentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListDeployments" }
func (startRequest) WaitFor(dex.Context, emq.ListDeploymentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input emq.ListDeploymentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[emq.ListDeploymentsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[emq.ListDeploymentsResult]
}

func (completeResult) GetStepType() string { return "CompleteListDeployments" }
func (completeResult) Execute(ctx dex.Context, result emq.ListDeploymentsResult) (*dex.StepDecision, error) {
	if err := deploymentListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
