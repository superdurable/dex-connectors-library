// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package job_runinspection runs Databricks getJobRun as one Dex Flow.
package job_runinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/databricks/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "DatabricksJobRunInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "databricks"
	operationStepType = "CallGetJobRun"
)

var jobRunAttribute = dex.DefineAttribute[databricks.JobRunStatus]("databricks-job_run")

// Flow runs GetJobRun once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection databricks.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection databricks.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{jobRunAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(databricks.NewGetJobRunStep(databricks.GetJobRunStepConfig[databricks.GetJobRunInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Databricks", Explanation: "Read a job_run from Databricks."},
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

// dex:field attribute-key:databricks-job_run value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readJobRun(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"databricks-job_run": outcome}}, nil
}

// dex:field attribute-key:databricks-job_run value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readJobRun(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"databricks-job_run": outcome}}, nil
}

func readJobRun(ctx dex.Context) (databricks.JobRunStatus, error) {
	outcome, err := jobRunAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return databricks.JobRunStatus{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input databricks.GetJobRunInput) databricks.GetJobRunInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetJobRun" }
func (startRequest) WaitFor(dex.Context, databricks.GetJobRunInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input databricks.GetJobRunInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[databricks.GetJobRunInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[databricks.GetJobRunResult]
}

func (completeResult) GetStepType() string { return "CompleteGetJobRun" }
func (completeResult) Execute(ctx dex.Context, result databricks.GetJobRunResult) (*dex.StepDecision, error) {
	if err := jobRunAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
