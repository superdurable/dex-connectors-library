// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package onlinestatus runs XingyAPI getInstanceOnlineStatus as one Dex Flow.
package onlinestatus

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/xingyapi/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "XingyAPIInstanceOnlineStatus"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "xingyapi"
	operationStepType = "CallGetInstanceOnlineStatus"
)

var instanceOnlineStatusOutcomeAttribute = dex.DefineAttribute[xingyapi.GetInstanceOnlineStatusOutput]("xingyapi-get-instance-online-status-outcome")

// Flow runs GetInstanceOnlineStatus once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection xingyapi.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection xingyapi.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{instanceOnlineStatusOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(xingyapi.NewGetInstanceOnlineStatusStep(xingyapi.GetInstanceOnlineStatusStepConfig[xingyapi.GetInstanceOnlineStatusInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "XingyAPI", Explanation: "Read XingyAPI instance login status for the supplied GUID."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Checked:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:xingyapi-get-instance-online-status-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetInstanceOnlineStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"xingyapi-get-instance-online-status-outcome": outcome}}, nil
}

// dex:field attribute-key:xingyapi-get-instance-online-status-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetInstanceOnlineStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"xingyapi-get-instance-online-status-outcome": outcome}}, nil
}

func readGetInstanceOnlineStatusOutcome(ctx dex.Context) (xingyapi.GetInstanceOnlineStatusOutput, error) {
	outcome, err := instanceOnlineStatusOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return xingyapi.GetInstanceOnlineStatusOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input xingyapi.GetInstanceOnlineStatusInput) xingyapi.GetInstanceOnlineStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetInstanceOnlineStatus" }
func (startRequest) WaitFor(dex.Context, xingyapi.GetInstanceOnlineStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input xingyapi.GetInstanceOnlineStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[xingyapi.GetInstanceOnlineStatusInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[xingyapi.GetInstanceOnlineStatusResult]
}

func (completeResult) GetStepType() string { return "CompleteGetInstanceOnlineStatus" }
func (completeResult) Execute(ctx dex.Context, result xingyapi.GetInstanceOnlineStatusResult) (*dex.StepDecision, error) {
	if err := instanceOnlineStatusOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
