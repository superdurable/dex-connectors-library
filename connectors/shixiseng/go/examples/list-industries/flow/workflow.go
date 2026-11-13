// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package industries runs Shixiseng listIndustries as one Dex Flow.
package industries

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/shixiseng/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ShixisengIndustries"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "shixiseng"
	operationStepType = "CallListIndustries"
)

var industriesOutcomeAttribute = dex.DefineAttribute[shixiseng.ListIndustriesOutput]("shixiseng-list-industries-outcome")

// Flow runs ListIndustries once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection shixiseng.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection shixiseng.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{industriesOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(shixiseng.NewListIndustriesStep(shixiseng.ListIndustriesStepConfig[shixiseng.ListIndustriesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Shixiseng", Explanation: "List public industry names for internship filters."},
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

// dex:field attribute-key:shixiseng-list-industries-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListIndustriesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shixiseng-list-industries-outcome": outcome}}, nil
}

// dex:field attribute-key:shixiseng-list-industries-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListIndustriesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shixiseng-list-industries-outcome": outcome}}, nil
}

func readListIndustriesOutcome(ctx dex.Context) (shixiseng.ListIndustriesOutput, error) {
	outcome, err := industriesOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return shixiseng.ListIndustriesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input shixiseng.ListIndustriesInput) shixiseng.ListIndustriesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListIndustries" }
func (startRequest) WaitFor(dex.Context, shixiseng.ListIndustriesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input shixiseng.ListIndustriesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[shixiseng.ListIndustriesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[shixiseng.ListIndustriesResult]
}

func (completeResult) GetStepType() string { return "CompleteListIndustries" }
func (completeResult) Execute(ctx dex.Context, result shixiseng.ListIndustriesResult) (*dex.StepDecision, error) {
	if err := industriesOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
