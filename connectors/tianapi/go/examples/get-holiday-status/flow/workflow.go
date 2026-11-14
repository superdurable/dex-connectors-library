// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package holidaylookup runs TianAPI getHolidayStatus as one Dex Flow.
package holidaylookup

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/tianapi/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TianAPIHolidayStatus"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "tianapi"
	operationStepType = "CallGetHolidayStatus"
)

var holidayStatusOutcomeAttribute = dex.DefineAttribute[tianapi.GetHolidayStatusOutput]("tianapi-get-holiday-status-outcome")

// Flow runs GetHolidayStatus once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection tianapi.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection tianapi.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{holidayStatusOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(tianapi.NewGetHolidayStatusStep(tianapi.GetHolidayStatusStepConfig[tianapi.GetHolidayStatusInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TianAPI", Explanation: "Read TianAPI holiday status for the supplied date."},
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

// dex:field attribute-key:tianapi-get-holiday-status-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetHolidayStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tianapi-get-holiday-status-outcome": outcome}}, nil
}

// dex:field attribute-key:tianapi-get-holiday-status-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetHolidayStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tianapi-get-holiday-status-outcome": outcome}}, nil
}

func readGetHolidayStatusOutcome(ctx dex.Context) (tianapi.GetHolidayStatusOutput, error) {
	outcome, err := holidayStatusOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tianapi.GetHolidayStatusOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input tianapi.GetHolidayStatusInput) tianapi.GetHolidayStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetHolidayStatus" }
func (startRequest) WaitFor(dex.Context, tianapi.GetHolidayStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input tianapi.GetHolidayStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tianapi.GetHolidayStatusInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[tianapi.GetHolidayStatusResult]
}

func (completeResult) GetStepType() string { return "CompleteGetHolidayStatus" }
func (completeResult) Execute(ctx dex.Context, result tianapi.GetHolidayStatusResult) (*dex.StepDecision, error) {
	if err := holidayStatusOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
