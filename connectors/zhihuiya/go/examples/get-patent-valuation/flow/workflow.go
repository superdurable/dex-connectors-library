// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package valuationread runs Zhihuiya getPatentValuation as one Dex Flow.
package valuationread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/zhihuiya/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ZhihuiyaPatentValuation"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "zhihuiya"
	operationStepType = "CallGetPatentValuation"
)

var valuationOutcomeAttribute = dex.DefineAttribute[zhihuiya.GetPatentValuationOutput]("zhihuiya-get-patent-valuation-outcome")

// Flow runs GetPatentValuation once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection zhihuiya.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection zhihuiya.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{valuationOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(zhihuiya.NewGetPatentValuationStep(zhihuiya.GetPatentValuationStepConfig[zhihuiya.GetPatentValuationInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Zhihuiya", Explanation: "Read one Zhihuiya patent valuation without abstracts or parties."},
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

// dex:field attribute-key:zhihuiya-get-patent-valuation-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetPatentValuationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zhihuiya-get-patent-valuation-outcome": outcome}}, nil
}

// dex:field attribute-key:zhihuiya-get-patent-valuation-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetPatentValuationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zhihuiya-get-patent-valuation-outcome": outcome}}, nil
}

func readGetPatentValuationOutcome(ctx dex.Context) (zhihuiya.GetPatentValuationOutput, error) {
	outcome, err := valuationOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return zhihuiya.GetPatentValuationOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input zhihuiya.GetPatentValuationInput) zhihuiya.GetPatentValuationInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the patent publication number to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetPatentValuation" }
func (startRequest) WaitFor(dex.Context, zhihuiya.GetPatentValuationInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input zhihuiya.GetPatentValuationInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[zhihuiya.GetPatentValuationInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the patent valuation."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[zhihuiya.GetPatentValuationResult]
}

func (completeResult) GetStepType() string { return "CompleteGetPatentValuation" }
func (completeResult) Execute(ctx dex.Context, result zhihuiya.GetPatentValuationResult) (*dex.StepDecision, error) {
	if err := valuationOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
