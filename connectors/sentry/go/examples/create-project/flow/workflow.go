// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createproject demonstrates bounded Sentry project creation.
package createproject

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sentry/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "SentryCreateProject"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "sentry"
)

var outcomeAttribute = dex.DefineAttribute[sentry.Project]("sentry-created-project")

// Request identifies the owning team and desired project.
type Request sentry.CreateProjectInput

// Flow creates one project and completes with its new metadata.
type Flow struct {
	dex.FlowDefaults
	connection sentry.Connection
}

// NewFlow binds a Sentry connection.
func NewFlow(connection sentry.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the created project result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the created project.
// dex:field attribute-key:sentry-created-project value-type:json editable:false description:"Created project"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedProject(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-created-project": value}}, nil
}

// GetDexDisplay returns the created project.
// dex:field attribute-key:sentry-created-project value-type:json editable:false description:"Created project"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedProject(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-created-project": value}}, nil
}
func readCreatedProject(ctx dex.Context) (sentry.Project, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sentry.Project{}, nil
	}
	return value, err
}

// GetSteps registers the project creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareProject{}),
		dex.DefineStep(sentry.NewCreateProjectStep(sentry.CreateProjectStepConfig[Request]{
			StepType: "CreateProject", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "sentry-project", GroupLabel: "Sentry project", Explanation: "Create the project under its owning Sentry team."},
			MapToOperationInput: func(request Request) sentry.CreateProjectInput { return sentry.CreateProjectInput(request) },
			Created:             sdkgo.GoTo(completeProject{}),
		})),
		dex.DefineStep(completeProject{}),
	}
}

// dex:group group-id:sentry-project-prepare group-label:"Prepare project"
// dex:explanation text:"Pass the typed project request into the provider creation."
type prepareProject struct{ dex.StepDefaults }

func (prepareProject) GetStepType() string { return "PrepareProject" }
func (prepareProject) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareProject) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateProject"), request), nil
}

// dex:group group-id:sentry-project-complete group-label:"Complete project creation"
// dex:explanation text:"Complete with Sentry's created project metadata."
type completeProject struct {
	dex.StepDefaultsNoWaitFor[sentry.CreateProjectResult]
}

func (completeProject) GetStepType() string { return "CompleteProject" }
func (completeProject) Execute(ctx dex.Context, result sentry.CreateProjectResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
