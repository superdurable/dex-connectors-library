// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package repositoryreleases

import (
	"errors"
	"time"

	githubconnector "github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const ConnectionName = "github-repository-releases"

var releasePageAttribute = dex.DefineAttribute[githubconnector.ListReleasesResult]("github-release-page")

// RepositoryReleasesFlow reads a single bounded provider page and retains the typed result.
// It is independent of the existing repository-changes Flow and has no shared mutable state.
type RepositoryReleasesFlow struct {
	dex.FlowDefaults
	connection githubconnector.Connection
}

// NewRepositoryReleasesFlow injects the configured read-only GitHub connection.
func NewRepositoryReleasesFlow(connection githubconnector.Connection) *RepositoryReleasesFlow {
	return &RepositoryReleasesFlow{connection: connection}
}

func (flow *RepositoryReleasesFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(githubconnector.NewListReleasesStep(githubconnector.ListReleasesStepConfig[githubconnector.ListReleasesInput]{
			StepType: "ListRepositoryReleases", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "github", GroupLabel: "GitHub", Explanation: "Read one page of release identities, publication dates and bounded notes."},
			MapToOperationInput: func(input githubconnector.ListReleasesInput) githubconnector.ListReleasesInput { return input },
			Listed:              sdkgo.GoTo(RecordReleasePage{}),
		})),
		dex.DefineStep(RecordReleasePage{}),
	}
}

func (*RepositoryReleasesFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{releasePageAttribute}}
}

func (flow *RepositoryReleasesFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetReleasePage, nil), dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetReleasePage provides typed retained state, including after the engine completes.
func (*RepositoryReleasesFlow) GetReleasePage(ctx dex.Context, _ dex.None) (*dex.RPCResult[githubconnector.ListReleasesResult], error) {
	result, err := releasePageAttribute.Get(ctx)
	var absent *dex.AttributeNotFoundError
	if errors.As(err, &absent) {
		return &dex.RPCResult[githubconnector.ListReleasesResult]{}, nil
	}
	return &dex.RPCResult[githubconnector.ListReleasesResult]{Output: result}, err
}

// dex:field attribute-key:github-release-page value-type:object editable:false description:"Bounded release page"
func (flow *RepositoryReleasesFlow) GetDexSummary(ctx dex.Context, input dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := flow.GetReleasePage(ctx, input)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"github-release-page": result.Output}}, nil
}

// dex:field attribute-key:github-release-page value-type:object editable:false description:"Release identities, notes, publication times and next page"
func (flow *RepositoryReleasesFlow) GetDexDisplay(ctx dex.Context, input dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := flow.GetReleasePage(ctx, input)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"github-release-page": result.Output}}, nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the exact typed query result and complete this page read."
type RecordReleasePage struct {
	dex.StepDefaultsNoWaitFor[githubconnector.ListReleasesResult]
}

func (RecordReleasePage) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: 10 * time.Second, ExecuteRetry: &dex.RetryPolicy{MaximumAttempts: 1}}
}

func (RecordReleasePage) Execute(ctx dex.Context, result githubconnector.ListReleasesResult) (*dex.StepDecision, error) {
	if err := releasePageAttribute.Set(ctx, result); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result), nil
}
