// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package teamcatalog runs ONES listTeams as one Dex Flow.
package teamcatalog

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/ones/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ONESTeamCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "ones"
	operationStepType = "CallListTeams"
)

var teamCatalogOutcomeAttribute = dex.DefineAttribute[ones.ListTeamsOutput]("ones-team-catalog-outcome")

// Flow runs ListTeams once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection ones.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection ones.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{teamCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(ones.NewListTeamsStep(ones.ListTeamsStepConfig[ones.ListTeamsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ONES", Explanation: "List team IDs accessible to the connected ONES API key."},
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

// dex:field attribute-key:ones-team-catalog-outcome value-type:json editable:false description:"Team catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTeamCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ones-team-catalog-outcome": outcome}}, nil
}

// dex:field attribute-key:ones-team-catalog-outcome value-type:json editable:false description:"Team catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTeamCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ones-team-catalog-outcome": outcome}}, nil
}

func readTeamCatalogOutcome(ctx dex.Context) (ones.ListTeamsOutput, error) {
	outcome, err := teamCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ones.ListTeamsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input ones.ListTeamsInput) ones.ListTeamsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListTeams" }
func (startRequest) WaitFor(dex.Context, ones.ListTeamsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input ones.ListTeamsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ones.ListTeamsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[ones.ListTeamsResult]
}

func (completeResult) GetStepType() string { return "CompleteListTeams" }
func (completeResult) Execute(ctx dex.Context, result ones.ListTeamsResult) (*dex.StepDecision, error) {
	if err := teamCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
