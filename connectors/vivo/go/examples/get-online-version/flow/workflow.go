// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package onlineversion runs Vivo getOnlineVersion as one Dex Flow.
package onlineversion

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/vivo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "VivoOnlineVersion"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "vivo"
	operationStepType = "CallGetOnlineVersion"
)

var onlineVersionOutcomeAttribute = dex.DefineAttribute[vivo.GetOnlineVersionOutput]("vivo-get-online-version-outcome")

// Flow runs GetOnlineVersion once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection vivo.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection vivo.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{onlineVersionOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(vivo.NewGetOnlineVersionStep(vivo.GetOnlineVersionStepConfig[vivo.GetOnlineVersionInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Vivo", Explanation: "Read the published vivo App Store version by package name."},
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

// dex:field attribute-key:vivo-get-online-version-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetOnlineVersionOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"vivo-get-online-version-outcome": outcome}}, nil
}

// dex:field attribute-key:vivo-get-online-version-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetOnlineVersionOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"vivo-get-online-version-outcome": outcome}}, nil
}

func readGetOnlineVersionOutcome(ctx dex.Context) (vivo.GetOnlineVersionOutput, error) {
	outcome, err := onlineVersionOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return vivo.GetOnlineVersionOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input vivo.GetOnlineVersionInput) vivo.GetOnlineVersionInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetOnlineVersion" }
func (startRequest) WaitFor(dex.Context, vivo.GetOnlineVersionInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input vivo.GetOnlineVersionInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[vivo.GetOnlineVersionInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[vivo.GetOnlineVersionResult]
}

func (completeResult) GetStepType() string { return "CompleteGetOnlineVersion" }
func (completeResult) Execute(ctx dex.Context, result vivo.GetOnlineVersionResult) (*dex.StepDecision, error) {
	if err := onlineVersionOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
