// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package demo runs Mailgun getEvents as one Dex Flow.
package demo

import (
	"github.com/superdurable/dex-connectors-library/connectors/mailgun"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MailgunDemo"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "mailgun"
	operationStepType = "CallGetEvents"
)

// Flow runs GetEvents once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection mailgun.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection mailgun.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(mailgun.NewGetEventsStep(mailgun.GetEventsStepConfig[mailgun.GetEventsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Mailgun", Explanation: "Call the provider's getEvents operation."},
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

// GetDexSummary identifies the example's static connection in Dex Web.
func (*Flow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"connection": ConnectionName}}, nil
}

// GetDexDisplay identifies the example's static connection in Dex Web.
func (*Flow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"connection": ConnectionName}}, nil
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input mailgun.GetEventsInput) mailgun.GetEventsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetEvents" }
func (startRequest) WaitFor(dex.Context, mailgun.GetEventsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input mailgun.GetEventsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[mailgun.GetEventsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[mailgun.GetEventsResult]
}

func (completeResult) GetStepType() string { return "CompleteGetEvents" }
func (completeResult) Execute(_ dex.Context, result mailgun.GetEventsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
