// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package projectcatalog runs Apifox getProjectAPISummary as one Dex Flow.
package projectcatalog

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/apifox/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ApifoxProjectAPICatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "apifox"
	operationStepType = "CallGetProjectAPISummary"
)

var projectAPISummaryOutcomeAttribute = dex.DefineAttribute[apifox.GetProjectAPISummaryOutput]("apifox-get-project-api-summary-outcome")

// Flow runs GetProjectAPISummary once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection apifox.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection apifox.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectAPISummaryOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(apifox.NewGetProjectAPISummaryStep(apifox.GetProjectAPISummaryStepConfig[apifox.GetProjectAPISummaryInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Apifox", Explanation: "Summarize an authorized project API definition."},
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

// dex:field attribute-key:apifox-get-project-api-summary-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetProjectAPISummaryOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"apifox-get-project-api-summary-outcome": outcome}}, nil
}

// dex:field attribute-key:apifox-get-project-api-summary-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetProjectAPISummaryOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"apifox-get-project-api-summary-outcome": outcome}}, nil
}

func readGetProjectAPISummaryOutcome(ctx dex.Context) (apifox.GetProjectAPISummaryOutput, error) {
	outcome, err := projectAPISummaryOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return apifox.GetProjectAPISummaryOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input apifox.GetProjectAPISummaryInput) apifox.GetProjectAPISummaryInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetProjectAPISummary" }
func (startRequest) WaitFor(dex.Context, apifox.GetProjectAPISummaryInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input apifox.GetProjectAPISummaryInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[apifox.GetProjectAPISummaryInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[apifox.GetProjectAPISummaryResult]
}

func (completeResult) GetStepType() string { return "CompleteGetProjectAPISummary" }
func (completeResult) Execute(ctx dex.Context, result apifox.GetProjectAPISummaryResult) (*dex.StepDecision, error) {
	if err := projectAPISummaryOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
