// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package storeread runs Weimob getStoreBasic as one Dex Flow.
package storeread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/weimob/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WeimobStoreRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "weimob"
	operationStepType = "CallGetStoreBasic"
)

var storeAttribute = dex.DefineAttribute[weimob.StoreBasic]("weimob-store-read")

// Flow runs GetStoreBasic once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection weimob.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection weimob.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{storeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(weimob.NewGetStoreBasicStep(weimob.GetStoreBasicStepConfig[weimob.GetStoreBasicInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Weimob", Explanation: "Read the authorized Weimob store."},
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

// dex:field attribute-key:weimob-store-read value-type:json editable:false description:"Weimob store information"
// GetDexSummary shows the last Weimob store information in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readStore(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"weimob-store-read": outcome}}, nil
}

// dex:field attribute-key:weimob-store-read value-type:json editable:false description:"Weimob store information"
// GetDexDisplay shows the last Weimob store information in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readStore(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"weimob-store-read": outcome}}, nil
}

func readStore(ctx dex.Context) (weimob.StoreBasic, error) {
	outcome, err := storeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return weimob.StoreBasic{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input weimob.GetStoreBasicInput) weimob.GetStoreBasicInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetStoreBasic" }
func (startRequest) WaitFor(dex.Context, weimob.GetStoreBasicInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input weimob.GetStoreBasicInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[weimob.GetStoreBasicInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[weimob.GetStoreBasicResult]
}

func (completeResult) GetStepType() string { return "CompleteGetStoreBasic" }
func (completeResult) Execute(ctx dex.Context, result weimob.GetStoreBasicResult) (*dex.StepDecision, error) {
	if err := storeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
