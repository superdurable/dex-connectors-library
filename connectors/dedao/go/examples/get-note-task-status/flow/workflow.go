// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package notetaskstatus runs Dedao getNoteTaskStatus as one Dex Flow.
package notetaskstatus

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/dedao/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "DedaoNoteTaskStatus"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "dedao"
	operationStepType = "CallGetNoteTaskStatus"
)

var taskStatusOutcomeAttribute = dex.DefineAttribute[dedao.GetNoteTaskStatusOutput]("dedao-get-note-task-status-outcome")

// Flow runs GetNoteTaskStatus once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection dedao.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection dedao.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{taskStatusOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(dedao.NewGetNoteTaskStatusStep(dedao.GetNoteTaskStatusStepConfig[dedao.GetNoteTaskStatusInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dedao", Explanation: "Read status for an existing asynchronous note task."},
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

// dex:field attribute-key:dedao-get-note-task-status-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetNoteTaskStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dedao-get-note-task-status-outcome": outcome}}, nil
}

// dex:field attribute-key:dedao-get-note-task-status-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetNoteTaskStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dedao-get-note-task-status-outcome": outcome}}, nil
}

func readGetNoteTaskStatusOutcome(ctx dex.Context) (dedao.GetNoteTaskStatusOutput, error) {
	outcome, err := taskStatusOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dedao.GetNoteTaskStatusOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input dedao.GetNoteTaskStatusInput) dedao.GetNoteTaskStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetNoteTaskStatus" }
func (startRequest) WaitFor(dex.Context, dedao.GetNoteTaskStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input dedao.GetNoteTaskStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dedao.GetNoteTaskStatusInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[dedao.GetNoteTaskStatusResult]
}

func (completeResult) GetStepType() string { return "CompleteGetNoteTaskStatus" }
func (completeResult) Execute(ctx dex.Context, result dedao.GetNoteTaskStatusResult) (*dex.StepDecision, error) {
	if err := taskStatusOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
