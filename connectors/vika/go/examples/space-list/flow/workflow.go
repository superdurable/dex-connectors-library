// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package spacelist runs Vika listSpaces as one Dex Flow.
package spacelist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/vika/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "VikaSpaces"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "vika"
	operationStepType = "CallListSpaces"
)

var spaceListOutcomeAttribute = dex.DefineAttribute[vika.SpacePage]("vika-space-list-outcome")

// Flow runs ListSpaces once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection vika.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection vika.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{spaceListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(vika.NewListSpacesStep(vika.ListSpacesStepConfig[vika.ListSpacesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Vika", Explanation: "List authorized Vika workspace metadata."},
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

// dex:field attribute-key:vika-space-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSpacesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"vika-space-list-outcome": outcome}}, nil
}

// dex:field attribute-key:vika-space-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSpacesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"vika-space-list-outcome": outcome}}, nil
}

func readSpacesOutcome(ctx dex.Context) (vika.SpacePage, error) {
	outcome, err := spaceListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return vika.SpacePage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input vika.ListSpacesInput) vika.ListSpacesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListSpaces" }
func (startRequest) WaitFor(dex.Context, vika.ListSpacesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input vika.ListSpacesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[vika.ListSpacesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[vika.ListSpacesResult]
}

func (completeResult) GetStepType() string { return "CompleteListSpaces" }
func (completeResult) Execute(ctx dex.Context, result vika.ListSpacesResult) (*dex.StepDecision, error) {
	if err := spaceListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
