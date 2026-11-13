// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package chargestatusflow runs Ping++ getChargeStatus as one Dex Flow.
package chargestatusflow

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/pingpp/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "PingppChargeStatus"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "pingpp"
	operationStepType = "CallGetChargeStatus"
)

var chargeStatusOutcomeAttribute = dex.DefineAttribute[pingpp.GetChargeStatusOutput]("pingpp-charge-status-outcome")

// Flow runs GetChargeStatus once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection pingpp.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection pingpp.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{chargeStatusOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(pingpp.NewGetChargeStatusStep(pingpp.GetChargeStatusStepConfig[pingpp.GetChargeStatusInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ping++", Explanation: "Read the status flags for the requested Charge."},
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

// dex:field attribute-key:pingpp-charge-status-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readChargeStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pingpp-charge-status-outcome": outcome}}, nil
}

// dex:field attribute-key:pingpp-charge-status-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readChargeStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pingpp-charge-status-outcome": outcome}}, nil
}

func readChargeStatusOutcome(ctx dex.Context) (pingpp.GetChargeStatusOutput, error) {
	outcome, err := chargeStatusOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pingpp.GetChargeStatusOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input pingpp.GetChargeStatusInput) pingpp.GetChargeStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetChargeStatus" }
func (startRequest) WaitFor(dex.Context, pingpp.GetChargeStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input pingpp.GetChargeStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pingpp.GetChargeStatusInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[pingpp.GetChargeStatusResult]
}

func (completeResult) GetStepType() string { return "CompleteGetChargeStatus" }
func (completeResult) Execute(ctx dex.Context, result pingpp.GetChargeStatusResult) (*dex.StepDecision, error) {
	if err := chargeStatusOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
