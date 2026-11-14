// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package systemvoices runs Unisound listSystemVoices as one Dex Flow.
package systemvoices

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/unisound/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "UnisoundSystemVoices"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "unisound"
	operationStepType = "CallListSystemVoices"
)

var systemVoicesOutcomeAttribute = dex.DefineAttribute[unisound.ListSystemVoicesOutput]("unisound-list-system-voices-outcome")

// Flow runs ListSystemVoices once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection unisound.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection unisound.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{systemVoicesOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(unisound.NewListSystemVoicesStep(unisound.ListSystemVoicesStepConfig[unisound.ListSystemVoicesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Unisound", Explanation: "List the predefined voices available to this account."},
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

// dex:field attribute-key:unisound-list-system-voices-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListSystemVoicesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"unisound-list-system-voices-outcome": outcome}}, nil
}

// dex:field attribute-key:unisound-list-system-voices-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListSystemVoicesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"unisound-list-system-voices-outcome": outcome}}, nil
}

func readListSystemVoicesOutcome(ctx dex.Context) (unisound.ListSystemVoicesOutput, error) {
	outcome, err := systemVoicesOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return unisound.ListSystemVoicesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input unisound.ListSystemVoicesInput) unisound.ListSystemVoicesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListSystemVoices" }
func (startRequest) WaitFor(dex.Context, unisound.ListSystemVoicesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input unisound.ListSystemVoicesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[unisound.ListSystemVoicesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[unisound.ListSystemVoicesResult]
}

func (completeResult) GetStepType() string { return "CompleteListSystemVoices" }
func (completeResult) Execute(ctx dex.Context, result unisound.ListSystemVoicesResult) (*dex.StepDecision, error) {
	if err := systemVoicesOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
