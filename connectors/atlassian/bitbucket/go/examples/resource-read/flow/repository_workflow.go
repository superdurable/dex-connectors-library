// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createRepositoryStepType = "CreateBitbucketRepository"

var createdRepositoryAttribute = dex.DefineAttribute[bitbucket.Repository]("bitbucket-created-repository")

// RepositoryCreateFlow creates a repository through a Bitbucket Mutation Step.
type RepositoryCreateFlow struct {
	dex.FlowDefaults
	connection bitbucket.Connection
}

// NewRepositoryCreateFlow binds the static provider connection.
func NewRepositoryCreateFlow(connection bitbucket.Connection) *RepositoryCreateFlow {
	return &RepositoryCreateFlow{connection: connection}
}

// GetFlowType returns the stable repository create Flow identity.
func (*RepositoryCreateFlow) GetFlowType() string { return "BitbucketRepositoryCreate" }

// GetPersistenceSchema registers the retained repository.
func (*RepositoryCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdRepositoryAttribute}}
}

// GetSteps registers the start, provider Mutation, and completion Steps.
func (flow *RepositoryCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRepositoryCreate{}),
		dex.DefineStep(bitbucket.NewCreateRepositoryStep(bitbucket.CreateRepositoryStepConfig[bitbucket.CreateRepositoryInput]{
			StepType: createRepositoryStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "bitbucket", GroupLabel: "Bitbucket", Explanation: "Create a repository in Bitbucket."},
			MapToOperationInput: mapToRepositoryInput,
			Created:             sdkgo.GoTo(completeRepositoryCreate{}),
		})),
		dex.DefineStep(completeRepositoryCreate{}),
	}
}

// GetRPCs exposes read-only Dex Web views of the created repository.
func (flow *RepositoryCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:bitbucket-created-repository value-type:json editable:false description:"Created repository"
// GetDexSummary shows the created repository in Dex Web.
func (*RepositoryCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	repository, err := readCreatedRepository(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-created-repository": repository}}, nil
}

// dex:field attribute-key:bitbucket-created-repository value-type:json editable:false description:"Created repository"
// GetDexDisplay shows the created repository in Dex Web.
func (*RepositoryCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	repository, err := readCreatedRepository(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-created-repository": repository}}, nil
}

func readCreatedRepository(ctx dex.Context) (bitbucket.Repository, error) {
	repository, err := createdRepositoryAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return bitbucket.Repository{}, nil
	}
	return repository, err
}

func mapToRepositoryInput(input bitbucket.CreateRepositoryInput) bitbucket.CreateRepositoryInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the workspace, repository slug, name, and visibility to the provider Step."
type startRepositoryCreate struct{ dex.StepDefaults }

func (startRepositoryCreate) GetStepType() string { return "StartRepositoryCreate" }
func (startRepositoryCreate) WaitFor(dex.Context, bitbucket.CreateRepositoryInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRepositoryCreate) Execute(_ dex.Context, input bitbucket.CreateRepositoryInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[bitbucket.CreateRepositoryInput](createRepositoryStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the created repository and complete the Flow."
type completeRepositoryCreate struct {
	dex.StepDefaultsNoWaitFor[bitbucket.CreateRepositoryResult]
}

func (completeRepositoryCreate) GetStepType() string { return "CompleteRepositoryCreate" }
func (completeRepositoryCreate) Execute(ctx dex.Context, result bitbucket.CreateRepositoryResult) (*dex.StepDecision, error) {
	if err := createdRepositoryAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*RepositoryCreateFlow)(nil)
