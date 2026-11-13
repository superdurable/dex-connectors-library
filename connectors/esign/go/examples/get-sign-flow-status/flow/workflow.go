// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package signflowstatus runs Esign getSignFlowStatus as one Dex Flow.
package signflowstatus

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/esign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "EsignSignFlowStatus"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "esign"
	operationStepType = "CallGetSignFlowStatus"
)

var signFlowStatusOutcomeAttribute = dex.DefineAttribute[esign.GetSignFlowStatusOutput]("esign-get-sign-flow-status-outcome")

// Flow runs GetSignFlowStatus once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection esign.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection esign.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{signFlowStatusOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(esign.NewGetSignFlowStatusStep(esign.GetSignFlowStatusStepConfig[esign.GetSignFlowStatusInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Esign", Explanation: "Call the provider's getSignFlowStatus operation."},
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

// dex:field attribute-key:esign-get-sign-flow-status-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetSignFlowStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"esign-get-sign-flow-status-outcome": outcome}}, nil
}

// dex:field attribute-key:esign-get-sign-flow-status-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetSignFlowStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"esign-get-sign-flow-status-outcome": outcome}}, nil
}

func readGetSignFlowStatusOutcome(ctx dex.Context) (esign.GetSignFlowStatusOutput, error) {
	outcome, err := signFlowStatusOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return esign.GetSignFlowStatusOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input esign.GetSignFlowStatusInput) esign.GetSignFlowStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetSignFlowStatus" }
func (startRequest) WaitFor(dex.Context, esign.GetSignFlowStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input esign.GetSignFlowStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[esign.GetSignFlowStatusInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[esign.GetSignFlowStatusResult]
}

func (completeResult) GetStepType() string { return "CompleteGetSignFlowStatus" }
func (completeResult) Execute(ctx dex.Context, result esign.GetSignFlowStatusResult) (*dex.StepDecision, error) {
	if err := signFlowStatusOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
