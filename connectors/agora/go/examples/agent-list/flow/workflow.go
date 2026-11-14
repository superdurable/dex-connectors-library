// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package agentlist runs Agora listRunningAgents as one Dex Flow.
package agentlist

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/agora/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AgoraAgentList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "agora"
	operationStepType = "CallListRunningAgents"
)

var agentsAttribute = dex.DefineAttribute[agora.RunningAgents]("agora-agent-list")

// Flow runs ListRunningAgents once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection agora.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection agora.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{agentsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(agora.NewListRunningAgentsStep(agora.ListRunningAgentsStepConfig[agora.ListRunningAgentsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Agora", Explanation: "List Agora running agents."},
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

// dex:field attribute-key:agora-agent-list value-type:json editable:false description:"Agora running agents"
// GetDexSummary shows the last Agora running agents in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAgents(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"agora-agent-list": outcome}}, nil
}

// dex:field attribute-key:agora-agent-list value-type:json editable:false description:"Agora running agents"
// GetDexDisplay shows the last Agora running agents in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAgents(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"agora-agent-list": outcome}}, nil
}

func readAgents(ctx dex.Context) (agora.RunningAgents, error) {
	outcome, err := agentsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return agora.RunningAgents{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input agora.ListRunningAgentsInput) agora.ListRunningAgentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListRunningAgents" }
func (startRequest) WaitFor(dex.Context, agora.ListRunningAgentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input agora.ListRunningAgentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[agora.ListRunningAgentsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[agora.ListRunningAgentsResult]
}

func (completeResult) GetStepType() string { return "CompleteListRunningAgents" }
func (completeResult) Execute(ctx dex.Context, result agora.ListRunningAgentsResult) (*dex.StepDecision, error) {
	if err := agentsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
