// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package pinterest runs Pinterest listPins as one Dex Flow.
package pins

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/pinterest/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "PinterestPinsList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "pinterest"
	operationStepType = "CallListPins"
)

var pinsAttribute = dex.DefineAttribute[pinterest.PinsPage]("pinterest-page")

// Flow runs ListPins once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection pinterest.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection pinterest.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{pinsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(pinterest.NewListPinsStep(pinterest.ListPinsStepConfig[pinterest.ListPinsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Pinterest", Explanation: "List pins available to the authorized Pinterest customer."},
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

// dex:field attribute-key:pinterest-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPins(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pinterest-page": outcome}}, nil
}

// dex:field attribute-key:pinterest-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPins(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pinterest-page": outcome}}, nil
}

func readPins(ctx dex.Context) (pinterest.PinsPage, error) {
	outcome, err := pinsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pinterest.PinsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input pinterest.ListPinsInput) pinterest.ListPinsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListPins" }
func (startRequest) WaitFor(dex.Context, pinterest.ListPinsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input pinterest.ListPinsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pinterest.ListPinsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[pinterest.ListPinsResult]
}

func (completeResult) GetStepType() string { return "CompleteListPins" }
func (completeResult) Execute(ctx dex.Context, result pinterest.ListPinsResult) (*dex.StepDecision, error) {
	if err := pinsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
