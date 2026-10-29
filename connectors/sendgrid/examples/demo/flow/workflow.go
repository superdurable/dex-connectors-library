// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package demo runs SendGrid getStats as one Dex Flow.
package demo

import (
	"github.com/superdurable/dex-connectors-library/connectors/sendgrid"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "SendGridDemo"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "sendgrid"
	operationStepType = "CallGetStats"
)

// Flow runs GetStats once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection sendgrid.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection sendgrid.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(sendgrid.NewGetStatsStep(sendgrid.GetStatsStepConfig[sendgrid.GetStatsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SendGrid", Explanation: "Call the provider's getStats operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input sendgrid.GetStatsInput) sendgrid.GetStatsInput { return input }

// dex:group group-id:demo group-label:"Demo"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetStats" }
func (startRequest) WaitFor(dex.Context, sendgrid.GetStatsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input sendgrid.GetStatsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sendgrid.GetStatsInput](operationStepType), input), nil
}

// dex:group group-id:demo group-label:"Demo"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[sendgrid.GetStatsResult]
}

func (completeResult) GetStepType() string { return "CompleteGetStats" }
func (completeResult) Execute(_ dex.Context, result sendgrid.GetStatsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
