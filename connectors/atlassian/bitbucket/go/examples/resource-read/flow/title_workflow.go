// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const updateTitleStepType = "UpdateBitbucketPullRequestTitle"

var updatedPullRequestAttribute = dex.DefineAttribute[bitbucket.PullRequest]("bitbucket-updated-pull-request")

// PullRequestTitleFlow replaces a pull request title through a Bitbucket Mutation Step.
type PullRequestTitleFlow struct {
	dex.FlowDefaults
	connection bitbucket.Connection
}

// NewPullRequestTitleFlow binds the static provider connection.
func NewPullRequestTitleFlow(connection bitbucket.Connection) *PullRequestTitleFlow {
	return &PullRequestTitleFlow{connection: connection}
}

// GetFlowType returns the stable title update Flow identity.
func (*PullRequestTitleFlow) GetFlowType() string { return "BitbucketPullRequestTitleUpdate" }

// GetPersistenceSchema registers the retained pull request.
func (*PullRequestTitleFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updatedPullRequestAttribute}}
}

// GetSteps registers the start, provider Mutation, and completion Steps.
func (flow *PullRequestTitleFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTitleUpdate{}),
		dex.DefineStep(bitbucket.NewUpdatePullRequestTitleStep(bitbucket.UpdatePullRequestTitleStepConfig[bitbucket.UpdatePullRequestTitleInput]{
			StepType: updateTitleStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "bitbucket", GroupLabel: "Bitbucket", Explanation: "Replace the pull request title in Bitbucket."},
			MapToOperationInput: mapToTitleInput,
			Updated:             sdkgo.GoTo(completeTitleUpdate{}),
		})),
		dex.DefineStep(completeTitleUpdate{}),
	}
}

// GetRPCs exposes read-only Dex Web views of the updated pull request.
func (flow *PullRequestTitleFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:bitbucket-updated-pull-request value-type:json editable:false description:"Updated pull request"
// GetDexSummary shows the updated pull request in Dex Web.
func (*PullRequestTitleFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	pullRequest, err := readUpdatedPullRequest(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-updated-pull-request": pullRequest}}, nil
}

// dex:field attribute-key:bitbucket-updated-pull-request value-type:json editable:false description:"Updated pull request"
// GetDexDisplay shows the updated pull request in Dex Web.
func (*PullRequestTitleFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	pullRequest, err := readUpdatedPullRequest(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-updated-pull-request": pullRequest}}, nil
}

func readUpdatedPullRequest(ctx dex.Context) (bitbucket.PullRequest, error) {
	pullRequest, err := updatedPullRequestAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return bitbucket.PullRequest{}, nil
	}
	return pullRequest, err
}

func mapToTitleInput(input bitbucket.UpdatePullRequestTitleInput) bitbucket.UpdatePullRequestTitleInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the pull request identity and new title to the provider Step."
type startTitleUpdate struct{ dex.StepDefaults }

func (startTitleUpdate) GetStepType() string { return "StartPullRequestTitleUpdate" }
func (startTitleUpdate) WaitFor(dex.Context, bitbucket.UpdatePullRequestTitleInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTitleUpdate) Execute(_ dex.Context, input bitbucket.UpdatePullRequestTitleInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[bitbucket.UpdatePullRequestTitleInput](updateTitleStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the updated pull request and complete the Flow."
type completeTitleUpdate struct {
	dex.StepDefaultsNoWaitFor[bitbucket.UpdatePullRequestTitleResult]
}

func (completeTitleUpdate) GetStepType() string { return "CompletePullRequestTitleUpdate" }
func (completeTitleUpdate) Execute(ctx dex.Context, result bitbucket.UpdatePullRequestTitleResult) (*dex.StepDecision, error) {
	if err := updatedPullRequestAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PullRequestTitleFlow)(nil)
