// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createPullRequestStepType = "CreateBitbucketPullRequest"

var createdPullRequestAttribute = dex.DefineAttribute[bitbucket.PullRequest]("bitbucket-created-pull-request")

// PullRequestCreateFlow creates a Bitbucket pull request through a Mutation Step.
type PullRequestCreateFlow struct {
	dex.FlowDefaults
	connection bitbucket.Connection
}

// NewPullRequestCreateFlow binds the static provider connection.
func NewPullRequestCreateFlow(connection bitbucket.Connection) *PullRequestCreateFlow {
	return &PullRequestCreateFlow{connection: connection}
}

// GetFlowType returns the stable pull request create Flow identity.
func (*PullRequestCreateFlow) GetFlowType() string { return "BitbucketPullRequestCreate" }

// GetPersistenceSchema registers the retained pull request.
func (*PullRequestCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdPullRequestAttribute}}
}

// GetSteps registers the start, provider Mutation, and completion Steps.
func (flow *PullRequestCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPullRequestCreate{}),
		dex.DefineStep(bitbucket.NewCreatePullRequestStep(bitbucket.CreatePullRequestStepConfig[bitbucket.CreatePullRequestInput]{
			StepType: createPullRequestStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "bitbucket", GroupLabel: "Bitbucket", Explanation: "Create a pull request between two repository branches."},
			MapToOperationInput: mapToPullRequestCreateInput,
			Created:             sdkgo.GoTo(completePullRequestCreate{}),
		})),
		dex.DefineStep(completePullRequestCreate{}),
	}
}

// GetRPCs exposes read-only Dex Web views of the created pull request.
func (flow *PullRequestCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:bitbucket-created-pull-request value-type:json editable:false description:"Created pull request"
// GetDexSummary shows the created pull request in Dex Web.
func (*PullRequestCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	pullRequest, err := readCreatedPullRequest(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-created-pull-request": pullRequest}}, nil
}

// dex:field attribute-key:bitbucket-created-pull-request value-type:json editable:false description:"Created pull request"
// GetDexDisplay shows the created pull request in Dex Web.
func (*PullRequestCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	pullRequest, err := readCreatedPullRequest(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-created-pull-request": pullRequest}}, nil
}

func readCreatedPullRequest(ctx dex.Context) (bitbucket.PullRequest, error) {
	pullRequest, err := createdPullRequestAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return bitbucket.PullRequest{}, nil
	}
	return pullRequest, err
}

func mapToPullRequestCreateInput(input bitbucket.CreatePullRequestInput) bitbucket.CreatePullRequestInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the workspace, repository, title, and branch names to the provider Step."
type startPullRequestCreate struct{ dex.StepDefaults }

func (startPullRequestCreate) GetStepType() string { return "StartPullRequestCreate" }
func (startPullRequestCreate) WaitFor(dex.Context, bitbucket.CreatePullRequestInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPullRequestCreate) Execute(_ dex.Context, input bitbucket.CreatePullRequestInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[bitbucket.CreatePullRequestInput](createPullRequestStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the created pull request and complete the Flow."
type completePullRequestCreate struct {
	dex.StepDefaultsNoWaitFor[bitbucket.CreatePullRequestResult]
}

func (completePullRequestCreate) GetStepType() string { return "CompletePullRequestCreate" }
func (completePullRequestCreate) Execute(ctx dex.Context, result bitbucket.CreatePullRequestResult) (*dex.StepDecision, error) {
	if err := createdPullRequestAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PullRequestCreateFlow)(nil)
