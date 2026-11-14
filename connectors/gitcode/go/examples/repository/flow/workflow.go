// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package repositoryflow runs GitCode getRepository as one Dex Flow.
package repositoryflow

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/gitcode/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GitCodeRepository"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "gitcode"
	operationStepType = "CallGetRepository"
)

var repositoryOutcomeAttribute = dex.DefineAttribute[gitcode.GetRepositoryOutput]("gitcode-repository-outcome")

// Flow runs GetRepository once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection gitcode.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection gitcode.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{repositoryOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(gitcode.NewGetRepositoryStep(gitcode.GetRepositoryStepConfig[gitcode.GetRepositoryInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "GitCode", Explanation: "Read the status flags for the requested Repository."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Fetched:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:gitcode-repository-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRepositoryOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gitcode-repository-outcome": outcome}}, nil
}

// dex:field attribute-key:gitcode-repository-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRepositoryOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gitcode-repository-outcome": outcome}}, nil
}

func readRepositoryOutcome(ctx dex.Context) (gitcode.GetRepositoryOutput, error) {
	outcome, err := repositoryOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return gitcode.GetRepositoryOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input gitcode.GetRepositoryInput) gitcode.GetRepositoryInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetRepository" }
func (startRequest) WaitFor(dex.Context, gitcode.GetRepositoryInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input gitcode.GetRepositoryInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[gitcode.GetRepositoryInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[gitcode.GetRepositoryResult]
}

func (completeResult) GetStepType() string { return "CompleteGetRepository" }
func (completeResult) Execute(ctx dex.Context, result gitcode.GetRepositoryResult) (*dex.StepDecision, error) {
	if err := repositoryOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
