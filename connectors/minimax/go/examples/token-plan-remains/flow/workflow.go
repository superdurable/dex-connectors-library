// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package tokenplanremains runs MiniMax Token Plan usage as one Dex Flow.
package tokenplanremains

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/minimax/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MiniMaxTokenPlanRemains"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "minimax"
	operationStepType = "CallGetTokenPlanRemains"
)

var tokenPlanRemainsOutcomeAttribute = dex.DefineAttribute[minimax.GetTokenPlanRemainsOutput]("minimax-token-plan-remains-outcome")

// Flow runs GetTokenPlanRemains once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection minimax.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection minimax.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tokenPlanRemainsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(minimax.NewGetTokenPlanRemainsStep(minimax.GetTokenPlanRemainsStepConfig[minimax.GetTokenPlanRemainsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "MiniMax", Explanation: "Read the provider's Token Plan quota summary."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Fetched:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:minimax-token-plan-remains-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTokenPlanRemainsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"minimax-token-plan-remains-outcome": outcome}}, nil
}

// dex:field attribute-key:minimax-token-plan-remains-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTokenPlanRemainsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"minimax-token-plan-remains-outcome": outcome}}, nil
}

func readTokenPlanRemainsOutcome(ctx dex.Context) (minimax.GetTokenPlanRemainsOutput, error) {
	outcome, err := tokenPlanRemainsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return minimax.GetTokenPlanRemainsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput passes the empty start input to the provider call.
func MapToOperationInput(input minimax.GetTokenPlanRemainsInput) minimax.GetTokenPlanRemainsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetTokenPlanRemains" }
func (startRequest) WaitFor(dex.Context, minimax.GetTokenPlanRemainsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input minimax.GetTokenPlanRemainsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[minimax.GetTokenPlanRemainsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[minimax.GetTokenPlanRemainsResult]
}

func (completeResult) GetStepType() string { return "CompleteGetTokenPlanRemains" }
func (completeResult) Execute(ctx dex.Context, result minimax.GetTokenPlanRemainsResult) (*dex.StepDecision, error) {
	if err := tokenPlanRemainsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
