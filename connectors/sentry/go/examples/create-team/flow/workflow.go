// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createteam demonstrates bounded Sentry team creation.
package createteam

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sentry/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "SentryCreateTeam"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "sentry"
)

var outcomeAttribute = dex.DefineAttribute[sentry.Team]("sentry-created-team")

// Request identifies the organization and desired team slug.
type Request sentry.CreateTeamInput

// Flow creates one team and completes with its new metadata.
type Flow struct {
	dex.FlowDefaults
	connection sentry.Connection
}

// NewFlow binds a Sentry connection.
func NewFlow(connection sentry.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the created team result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the created team.
// dex:field attribute-key:sentry-created-team value-type:json editable:false description:"Created team"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedTeam(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-created-team": value}}, nil
}

// GetDexDisplay returns the created team.
// dex:field attribute-key:sentry-created-team value-type:json editable:false description:"Created team"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedTeam(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-created-team": value}}, nil
}
func readCreatedTeam(ctx dex.Context) (sentry.Team, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sentry.Team{}, nil
	}
	return value, err
}

// GetSteps registers the team creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareTeam{}),
		dex.DefineStep(sentry.NewCreateTeamStep(sentry.CreateTeamStepConfig[Request]{
			StepType: "CreateTeam", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "sentry-team", GroupLabel: "Sentry team", Explanation: "Create a named team in the selected Sentry organization."},
			MapToOperationInput: func(request Request) sentry.CreateTeamInput { return sentry.CreateTeamInput(request) },
			Created:             sdkgo.GoTo(completeTeam{}),
		})),
		dex.DefineStep(completeTeam{}),
	}
}

// dex:group group-id:sentry-team-prepare group-label:"Prepare team"
// dex:explanation text:"Pass the typed team request into the provider creation."
type prepareTeam struct{ dex.StepDefaults }

func (prepareTeam) GetStepType() string { return "PrepareTeam" }
func (prepareTeam) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareTeam) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateTeam"), request), nil
}

// dex:group group-id:sentry-team-complete group-label:"Complete team creation"
// dex:explanation text:"Complete with Sentry's created team metadata."
type completeTeam struct {
	dex.StepDefaultsNoWaitFor[sentry.CreateTeamResult]
}

func (completeTeam) GetStepType() string { return "CompleteTeam" }
func (completeTeam) Execute(ctx dex.Context, result sentry.CreateTeamResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
