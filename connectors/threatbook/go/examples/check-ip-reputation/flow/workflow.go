// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package iplookup runs ThreatBook checkIPReputation as one Dex Flow.
package iplookup

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/threatbook/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ThreatBookIPReputation"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "threatbook"
	operationStepType = "CallCheckIPReputation"
)

var ipReputationOutcomeAttribute = dex.DefineAttribute[threatbook.CheckIPReputationOutput]("threatbook-check-ip-reputation-outcome")

// Flow runs CheckIPReputation once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection threatbook.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection threatbook.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{ipReputationOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(threatbook.NewCheckIPReputationStep(threatbook.CheckIPReputationStepConfig[threatbook.CheckIPReputationInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ThreatBook", Explanation: "Read ThreatBook inbound risk verdict for the supplied IP address."},
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

// dex:field attribute-key:threatbook-check-ip-reputation-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCheckIPReputationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"threatbook-check-ip-reputation-outcome": outcome}}, nil
}

// dex:field attribute-key:threatbook-check-ip-reputation-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCheckIPReputationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"threatbook-check-ip-reputation-outcome": outcome}}, nil
}

func readCheckIPReputationOutcome(ctx dex.Context) (threatbook.CheckIPReputationOutput, error) {
	outcome, err := ipReputationOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return threatbook.CheckIPReputationOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input threatbook.CheckIPReputationInput) threatbook.CheckIPReputationInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartCheckIPReputation" }
func (startRequest) WaitFor(dex.Context, threatbook.CheckIPReputationInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input threatbook.CheckIPReputationInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[threatbook.CheckIPReputationInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[threatbook.CheckIPReputationResult]
}

func (completeResult) GetStepType() string { return "CompleteCheckIPReputation" }
func (completeResult) Execute(ctx dex.Context, result threatbook.CheckIPReputationResult) (*dex.StepDecision, error) {
	if err := ipReputationOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
