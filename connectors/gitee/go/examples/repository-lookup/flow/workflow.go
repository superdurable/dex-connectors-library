// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package repositorylookup runs Gitee getRepository as one Dex Flow.
package repositorylookup

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/gitee/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GiteeRepositoryLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "gitee"
	operationStepType = "CallGetRepository"
)

var repositoryLookupOutcomeAttribute = dex.DefineAttribute[gitee.GetRepositoryOutput]("gitee-repository-lookup-outcome")

// Flow runs GetRepository once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection gitee.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection gitee.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{repositoryLookupOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(gitee.NewGetRepositoryStep(gitee.GetRepositoryStepConfig[gitee.GetRepositoryInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Gitee", Explanation: "Call the provider's getRepository operation."},
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

// dex:field attribute-key:gitee-repository-lookup-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRepositoryLookupOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gitee-repository-lookup-outcome": outcome}}, nil
}

// dex:field attribute-key:gitee-repository-lookup-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRepositoryLookupOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gitee-repository-lookup-outcome": outcome}}, nil
}

func readRepositoryLookupOutcome(ctx dex.Context) (gitee.GetRepositoryOutput, error) {
	outcome, err := repositoryLookupOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return gitee.GetRepositoryOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input gitee.GetRepositoryInput) gitee.GetRepositoryInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetRepository" }
func (startRequest) WaitFor(dex.Context, gitee.GetRepositoryInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input gitee.GetRepositoryInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[gitee.GetRepositoryInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[gitee.GetRepositoryResult]
}

func (completeResult) GetStepType() string { return "CompleteGetRepository" }
func (completeResult) Execute(ctx dex.Context, result gitee.GetRepositoryResult) (*dex.StepDecision, error) {
	if err := repositoryLookupOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
