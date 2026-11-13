// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package teamcatalog runs Rainbond listTeams as one Dex Flow.
package teamcatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/rainbond/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "RainbondTeamCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "rainbond"
	operationStepType = "CallListTeams"
)

var teamCatalogOutcomeAttribute = dex.DefineAttribute[rainbond.ListTeamsOutput]("rainbond-team-catalog-outcome")

// Flow runs ListTeams once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection rainbond.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection rainbond.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{teamCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(rainbond.NewListTeamsStep(rainbond.ListTeamsStepConfig[rainbond.ListTeamsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Rainbond", Explanation: "Read one team page with IDs, names, active flags, and total count."},
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

// dex:field attribute-key:rainbond-team-catalog-outcome value-type:json editable:false description:"Team catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTeamCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"rainbond-team-catalog-outcome": outcome}}, nil
}

// dex:field attribute-key:rainbond-team-catalog-outcome value-type:json editable:false description:"Team catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTeamCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"rainbond-team-catalog-outcome": outcome}}, nil
}

func readTeamCatalogOutcome(ctx dex.Context) (rainbond.ListTeamsOutput, error) {
	outcome, err := teamCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return rainbond.ListTeamsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input rainbond.ListTeamsInput) rainbond.ListTeamsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListTeams" }
func (startRequest) WaitFor(dex.Context, rainbond.ListTeamsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input rainbond.ListTeamsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[rainbond.ListTeamsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[rainbond.ListTeamsResult]
}

func (completeResult) GetStepType() string { return "CompleteListTeams" }
func (completeResult) Execute(ctx dex.Context, result rainbond.ListTeamsResult) (*dex.StepDecision, error) {
	if err := teamCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
