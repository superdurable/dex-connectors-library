// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package agentcatalog runs Useasy listAgents as one Dex Flow.
package agentcatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/useasy/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "UseasyAgentCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "useasy"
	operationStepType = "CallListAgents"
)

var agentCatalogOutcomeAttribute = dex.DefineAttribute[useasy.ListAgentsOutput]("useasy-agent-directory-outcome")

// Flow runs ListAgents once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection useasy.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection useasy.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{agentCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(useasy.NewListAgentsStep(useasy.ListAgentsStepConfig[useasy.ListAgentsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Useasy", Explanation: "Read one agent page with IDs, work numbers, enabled states, and page counts."},
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

// dex:field attribute-key:useasy-agent-directory-outcome value-type:json editable:false description:"Agent catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAgentCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"useasy-agent-directory-outcome": outcome}}, nil
}

// dex:field attribute-key:useasy-agent-directory-outcome value-type:json editable:false description:"Agent catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAgentCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"useasy-agent-directory-outcome": outcome}}, nil
}

func readAgentCatalogOutcome(ctx dex.Context) (useasy.ListAgentsOutput, error) {
	outcome, err := agentCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return useasy.ListAgentsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input useasy.ListAgentsInput) useasy.ListAgentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListAgents" }
func (startRequest) WaitFor(dex.Context, useasy.ListAgentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input useasy.ListAgentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[useasy.ListAgentsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[useasy.ListAgentsResult]
}

func (completeResult) GetStepType() string { return "CompleteListAgents" }
func (completeResult) Execute(ctx dex.Context, result useasy.ListAgentsResult) (*dex.StepDecision, error) {
	if err := agentCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
