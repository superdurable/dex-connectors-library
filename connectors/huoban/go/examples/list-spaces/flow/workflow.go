// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package spaceslist runs Huoban listSpaces as one Dex Flow.
package spaceslist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/huoban/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "HuobanSpaces"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "huoban"
	operationStepType = "CallListSpaces"
)

var spacesOutcomeAttribute = dex.DefineAttribute[huoban.ListSpacesOutput]("huoban-list-spaces-outcome")

// Flow runs ListSpaces once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection huoban.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection huoban.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{spacesOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(huoban.NewListSpacesStep(huoban.ListSpacesStepConfig[huoban.ListSpacesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Huoban", Explanation: "Call the provider's listSpaces operation."},
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

// dex:field attribute-key:huoban-list-spaces-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListSpacesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"huoban-list-spaces-outcome": outcome}}, nil
}

// dex:field attribute-key:huoban-list-spaces-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListSpacesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"huoban-list-spaces-outcome": outcome}}, nil
}

func readListSpacesOutcome(ctx dex.Context) (huoban.ListSpacesOutput, error) {
	outcome, err := spacesOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return huoban.ListSpacesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input huoban.ListSpacesInput) huoban.ListSpacesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListSpaces" }
func (startRequest) WaitFor(dex.Context, huoban.ListSpacesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input huoban.ListSpacesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[huoban.ListSpacesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[huoban.ListSpacesResult]
}

func (completeResult) GetStepType() string { return "CompleteListSpaces" }
func (completeResult) Execute(ctx dex.Context, result huoban.ListSpacesResult) (*dex.StepDecision, error) {
	if err := spacesOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
