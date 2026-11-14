// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package pushreport runs Getui getDailyPushReport as one Dex Flow.
package pushreport

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/getui/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GetuiDailyPushReport"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "getui"
	operationStepType = "CallGetDailyPushReport"
)

var dailyPushReportOutcomeAttribute = dex.DefineAttribute[getui.GetDailyPushReportOutput]("getui-get-daily-push-report-outcome")

// Flow runs GetDailyPushReport once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection getui.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection getui.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{dailyPushReportOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(getui.NewGetDailyPushReportStep(getui.GetDailyPushReportStepConfig[getui.GetDailyPushReportInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Getui", Explanation: "Read Getui daily push totals for the supplied date."},
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

// dex:field attribute-key:getui-get-daily-push-report-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetDailyPushReportOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"getui-get-daily-push-report-outcome": outcome}}, nil
}

// dex:field attribute-key:getui-get-daily-push-report-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetDailyPushReportOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"getui-get-daily-push-report-outcome": outcome}}, nil
}

func readGetDailyPushReportOutcome(ctx dex.Context) (getui.GetDailyPushReportOutput, error) {
	outcome, err := dailyPushReportOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return getui.GetDailyPushReportOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input getui.GetDailyPushReportInput) getui.GetDailyPushReportInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetDailyPushReport" }
func (startRequest) WaitFor(dex.Context, getui.GetDailyPushReportInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input getui.GetDailyPushReportInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[getui.GetDailyPushReportInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[getui.GetDailyPushReportResult]
}

func (completeResult) GetStepType() string { return "CompleteGetDailyPushReport" }
func (completeResult) Execute(ctx dex.Context, result getui.GetDailyPushReportResult) (*dex.StepDecision, error) {
	if err := dailyPushReportOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
