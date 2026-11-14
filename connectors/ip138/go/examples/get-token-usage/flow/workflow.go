// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package tokenusage runs iP138 getTokenUsage as one Dex Flow.
package tokenusage

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/ip138/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "iP138TokenUsage"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "ip138"
	operationStepType = "CallGetTokenUsage"
)

var tokenUsageOutcomeAttribute = dex.DefineAttribute[ip138.GetTokenUsageOutput]("ip138-get-token-usage-outcome")

// Flow runs GetTokenUsage once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection ip138.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection ip138.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tokenUsageOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(ip138.NewGetTokenUsageStep(ip138.GetTokenUsageStepConfig[ip138.GetTokenUsageInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "iP138", Explanation: "Read iP138 token usage status for the connected token."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ip138-get-token-usage-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetTokenUsageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ip138-get-token-usage-outcome": outcome}}, nil
}

// dex:field attribute-key:ip138-get-token-usage-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetTokenUsageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ip138-get-token-usage-outcome": outcome}}, nil
}

func readGetTokenUsageOutcome(ctx dex.Context) (ip138.GetTokenUsageOutput, error) {
	outcome, err := tokenUsageOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ip138.GetTokenUsageOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input ip138.GetTokenUsageInput) ip138.GetTokenUsageInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetTokenUsage" }
func (startRequest) WaitFor(dex.Context, ip138.GetTokenUsageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input ip138.GetTokenUsageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ip138.GetTokenUsageInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[ip138.GetTokenUsageResult]
}

func (completeResult) GetStepType() string { return "CompleteGetTokenUsage" }
func (completeResult) Execute(ctx dex.Context, result ip138.GetTokenUsageResult) (*dex.StepDecision, error) {
	if err := tokenUsageOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
