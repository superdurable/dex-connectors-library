// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createBranchStepType = "CreateBitbucketBranch"

var createdBranchAttribute = dex.DefineAttribute[bitbucket.Branch]("bitbucket-created-branch")

// BranchCreateFlow creates a branch at an exact commit hash.
type BranchCreateFlow struct {
	dex.FlowDefaults
	connection bitbucket.Connection
}

// NewBranchCreateFlow binds the static provider connection.
func NewBranchCreateFlow(connection bitbucket.Connection) *BranchCreateFlow {
	return &BranchCreateFlow{connection: connection}
}

// GetFlowType returns the stable branch create Flow identity.
func (*BranchCreateFlow) GetFlowType() string { return "BitbucketBranchCreate" }

// GetPersistenceSchema registers the retained branch.
func (*BranchCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdBranchAttribute}}
}

// GetSteps registers the start, provider Mutation, and completion Steps.
func (flow *BranchCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startBranchCreate{}),
		dex.DefineStep(bitbucket.NewCreateBranchStep(bitbucket.CreateBranchStepConfig[bitbucket.CreateBranchInput]{
			StepType: createBranchStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "bitbucket", GroupLabel: "Bitbucket", Explanation: "Create a repository branch at an exact commit."},
			MapToOperationInput: mapToBranchCreateInput,
			Created:             sdkgo.GoTo(completeBranchCreate{}),
		})),
		dex.DefineStep(completeBranchCreate{}),
	}
}

// GetRPCs exposes read-only Dex Web views of the created branch.
func (flow *BranchCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:bitbucket-created-branch value-type:json editable:false description:"Created branch"
// GetDexSummary shows the created branch in Dex Web.
func (*BranchCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	branch, err := readCreatedBranch(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-created-branch": branch}}, nil
}

// dex:field attribute-key:bitbucket-created-branch value-type:json editable:false description:"Created branch"
// GetDexDisplay shows the created branch in Dex Web.
func (*BranchCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	branch, err := readCreatedBranch(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-created-branch": branch}}, nil
}

func readCreatedBranch(ctx dex.Context) (bitbucket.Branch, error) {
	branch, err := createdBranchAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return bitbucket.Branch{}, nil
	}
	return branch, err
}

func mapToBranchCreateInput(input bitbucket.CreateBranchInput) bitbucket.CreateBranchInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the branch name and full target commit hash to the provider Step."
type startBranchCreate struct{ dex.StepDefaults }

func (startBranchCreate) GetStepType() string { return "StartBranchCreate" }
func (startBranchCreate) WaitFor(dex.Context, bitbucket.CreateBranchInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startBranchCreate) Execute(_ dex.Context, input bitbucket.CreateBranchInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[bitbucket.CreateBranchInput](createBranchStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the created branch and complete the Flow."
type completeBranchCreate struct {
	dex.StepDefaultsNoWaitFor[bitbucket.CreateBranchResult]
}

func (completeBranchCreate) GetStepType() string { return "CompleteBranchCreate" }
func (completeBranchCreate) Execute(ctx dex.Context, result bitbucket.CreateBranchResult) (*dex.StepDecision, error) {
	if err := createdBranchAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*BranchCreateFlow)(nil)
