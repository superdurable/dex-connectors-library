// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package opportunityinspection runs Lever getOpportunity as one Dex Flow.
package opportunityinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/lever/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "LeverOpportunityInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "lever"
	operationStepType = "CallGetOpportunity"
)

var opportunityAttribute = dex.DefineAttribute[lever.OpportunityStage]("lever-opportunity")

// Flow runs GetOpportunity once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection lever.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection lever.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{opportunityAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(lever.NewGetOpportunityStep(lever.GetOpportunityStepConfig[lever.GetOpportunityInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Lever", Explanation: "Read a opportunity from Lever."},
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

// dex:field attribute-key:lever-opportunity value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOpportunity(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lever-opportunity": outcome}}, nil
}

// dex:field attribute-key:lever-opportunity value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOpportunity(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lever-opportunity": outcome}}, nil
}

func readOpportunity(ctx dex.Context) (lever.OpportunityStage, error) {
	outcome, err := opportunityAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return lever.OpportunityStage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input lever.GetOpportunityInput) lever.GetOpportunityInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetOpportunity" }
func (startRequest) WaitFor(dex.Context, lever.GetOpportunityInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input lever.GetOpportunityInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[lever.GetOpportunityInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[lever.GetOpportunityResult]
}

func (completeResult) GetStepType() string { return "CompleteGetOpportunity" }
func (completeResult) Execute(ctx dex.Context, result lever.GetOpportunityResult) (*dex.StepDecision, error) {
	if err := opportunityAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
