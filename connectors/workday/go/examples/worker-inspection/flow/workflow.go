// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package workerinspection runs Workday getWorker as one Dex Flow.
package workerinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/workday/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WorkdayWorkerInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "workday"
	operationStepType = "CallGetWorker"
)

var workerAttribute = dex.DefineAttribute[workday.WorkerSummary]("workday-worker")

// Flow runs GetWorker once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection workday.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection workday.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{workerAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(workday.NewGetWorkerStep(workday.GetWorkerStepConfig[workday.GetWorkerInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Workday", Explanation: "Read the selected worker profile from Workday."},
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

// dex:field attribute-key:workday-worker value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readWorker(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"workday-worker": outcome}}, nil
}

// dex:field attribute-key:workday-worker value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readWorker(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"workday-worker": outcome}}, nil
}

func readWorker(ctx dex.Context) (workday.WorkerSummary, error) {
	outcome, err := workerAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return workday.WorkerSummary{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input workday.GetWorkerInput) workday.GetWorkerInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetWorker" }
func (startRequest) WaitFor(dex.Context, workday.GetWorkerInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input workday.GetWorkerInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[workday.GetWorkerInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[workday.GetWorkerResult]
}

func (completeResult) GetStepType() string { return "CompleteGetWorker" }
func (completeResult) Execute(ctx dex.Context, result workday.GetWorkerResult) (*dex.StepDecision, error) {
	if err := workerAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
