// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package demo runs SendGrid getStats as one Dex Flow.
package demo

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/sendgrid/go"
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

var demoOutcomeAttribute = dex.DefineAttribute[sendgrid.StatsOutput]("sendgrid-demo-outcome")

// Flow runs GetStats once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection sendgrid.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection sendgrid.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{demoOutcomeAttribute}}
}

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

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:sendgrid-demo-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDemoOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-demo-outcome": outcome}}, nil
}

// dex:field attribute-key:sendgrid-demo-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDemoOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-demo-outcome": outcome}}, nil
}

func readDemoOutcome(ctx dex.Context) (sendgrid.StatsOutput, error) {
	outcome, err := demoOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sendgrid.StatsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input sendgrid.GetStatsInput) sendgrid.GetStatsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetStats" }
func (startRequest) WaitFor(dex.Context, sendgrid.GetStatsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input sendgrid.GetStatsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sendgrid.GetStatsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[sendgrid.GetStatsResult]
}

func (completeResult) GetStepType() string { return "CompleteGetStats" }
func (completeResult) Execute(ctx dex.Context, result sendgrid.GetStatsResult) (*dex.StepDecision, error) {
	if err := demoOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
