// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package useronlinestatus runs RongCloud checkUserOnline as one Dex Flow.
package useronlinestatus

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/rongcloud/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "RongCloudUserOnlineStatus"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "rongcloud"
	operationStepType = "CallCheckUserOnline"
)

var userOnlineStatusOutcomeAttribute = dex.DefineAttribute[rongcloud.CheckUserOnlineOutput]("rongcloud-user-online-status-outcome")

// Flow runs CheckUserOnline once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection rongcloud.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection rongcloud.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{userOnlineStatusOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(rongcloud.NewCheckUserOnlineStep(rongcloud.CheckUserOnlineStepConfig[rongcloud.CheckUserOnlineInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "RongCloud", Explanation: "Call the provider's checkUserOnline operation."},
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

// dex:field attribute-key:rongcloud-user-online-status-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUserOnlineStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"rongcloud-user-online-status-outcome": outcome}}, nil
}

// dex:field attribute-key:rongcloud-user-online-status-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUserOnlineStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"rongcloud-user-online-status-outcome": outcome}}, nil
}

func readUserOnlineStatusOutcome(ctx dex.Context) (rongcloud.CheckUserOnlineOutput, error) {
	outcome, err := userOnlineStatusOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return rongcloud.CheckUserOnlineOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input rongcloud.CheckUserOnlineInput) rongcloud.CheckUserOnlineInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartCheckUserOnline" }
func (startRequest) WaitFor(dex.Context, rongcloud.CheckUserOnlineInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input rongcloud.CheckUserOnlineInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[rongcloud.CheckUserOnlineInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[rongcloud.CheckUserOnlineResult]
}

func (completeResult) GetStepType() string { return "CompleteCheckUserOnline" }
func (completeResult) Execute(ctx dex.Context, result rongcloud.CheckUserOnlineResult) (*dex.StepDecision, error) {
	if err := userOnlineStatusOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
