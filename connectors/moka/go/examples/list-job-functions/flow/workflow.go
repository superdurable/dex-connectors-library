// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package listjobfunctions runs Moka listJobFunctions as one Dex Flow.
package listjobfunctions

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/moka/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MokaJobFunctionList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "moka"
	operationStepType = "CallListJobFunctions"
)

var functionsOutcomeAttribute = dex.DefineAttribute[moka.ListJobFunctionsOutput]("moka-list-job-functions-outcome")

// Flow runs ListJobFunctions once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection moka.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection moka.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{functionsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(moka.NewListJobFunctionsStep(moka.ListJobFunctionsStepConfig[moka.ListJobFunctionsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Moka", Explanation: "Call the provider's listJobFunctions operation."},
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

// dex:field attribute-key:moka-list-job-functions-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListJobFunctionsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"moka-list-job-functions-outcome": outcome}}, nil
}

// dex:field attribute-key:moka-list-job-functions-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListJobFunctionsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"moka-list-job-functions-outcome": outcome}}, nil
}

func readListJobFunctionsOutcome(ctx dex.Context) (moka.ListJobFunctionsOutput, error) {
	outcome, err := functionsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return moka.ListJobFunctionsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input moka.ListJobFunctionsInput) moka.ListJobFunctionsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListJobFunctions" }
func (startRequest) WaitFor(dex.Context, moka.ListJobFunctionsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input moka.ListJobFunctionsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[moka.ListJobFunctionsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[moka.ListJobFunctionsResult]
}

func (completeResult) GetStepType() string { return "CompleteListJobFunctions" }
func (completeResult) Execute(ctx dex.Context, result moka.ListJobFunctionsResult) (*dex.StepDecision, error) {
	if err := functionsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
