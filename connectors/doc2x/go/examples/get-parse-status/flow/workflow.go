// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package parsestatus runs Doc2X getParseStatus as one Dex Flow.
package parsestatus

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/doc2x/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "Doc2XParseStatus"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "doc2x"
	operationStepType = "CallGetParseStatus"
)

var parseStatusOutcomeAttribute = dex.DefineAttribute[doc2x.GetParseStatusOutput]("doc2x-get-parse-status-outcome")

// Flow runs GetParseStatus once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection doc2x.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection doc2x.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{parseStatusOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(doc2x.NewGetParseStatusStep(doc2x.GetParseStatusStepConfig[doc2x.GetParseStatusInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Doc2X", Explanation: "Read an existing PDF parsing task state and progress."},
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

// dex:field attribute-key:doc2x-get-parse-status-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetParseStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"doc2x-get-parse-status-outcome": outcome}}, nil
}

// dex:field attribute-key:doc2x-get-parse-status-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetParseStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"doc2x-get-parse-status-outcome": outcome}}, nil
}

func readGetParseStatusOutcome(ctx dex.Context) (doc2x.GetParseStatusOutput, error) {
	outcome, err := parseStatusOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return doc2x.GetParseStatusOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input doc2x.GetParseStatusInput) doc2x.GetParseStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetParseStatus" }
func (startRequest) WaitFor(dex.Context, doc2x.GetParseStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input doc2x.GetParseStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[doc2x.GetParseStatusInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[doc2x.GetParseStatusResult]
}

func (completeResult) GetStepType() string { return "CompleteGetParseStatus" }
func (completeResult) Execute(ctx dex.Context, result doc2x.GetParseStatusResult) (*dex.StepDecision, error) {
	if err := parseStatusOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
