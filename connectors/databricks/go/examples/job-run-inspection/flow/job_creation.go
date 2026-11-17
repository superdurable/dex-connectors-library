// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package job_runinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/databricks/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// JobCreationFlowType is the stable Flow identity for creating a notebook job.
const JobCreationFlowType = "DatabricksJobCreation"

var createdJobAttribute = dex.DefineAttribute[databricks.JobSummary]("databricks-created-job")

// JobCreationFlow creates one job from an explicit Dex Web start request.
type JobCreationFlow struct {
	dex.FlowDefaults
	connection databricks.Connection
}

// NewJobCreationFlow binds a Databricks connection for the job creation example.
func NewJobCreationFlow(connection databricks.Connection) *JobCreationFlow {
	return &JobCreationFlow{connection: connection}
}

func (*JobCreationFlow) GetFlowType() string { return JobCreationFlowType }

func (*JobCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdJobAttribute}}
}

func (flow *JobCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startJobCreation{}),
		dex.DefineStep(databricks.NewCreateJobStep(databricks.CreateJobStepConfig[databricks.CreateJobInput]{
			StepType: "CallCreateJob", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Databricks", Explanation: "Create a notebook job with bounded sends across unknown outcomes."},
			Connection:          flow.connection,
			MapToOperationInput: func(input databricks.CreateJobInput) databricks.CreateJobInput { return input },
			Created:             sdkgo.GoTo(completeJobCreation{}),
		})),
		dex.DefineStep(completeJobCreation{}),
	}
}

func (flow *JobCreationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:databricks-created-job value-type:json editable:false description:"Created job identity"
// GetDexSummary shows the confirmed job ID and name.
func (*JobCreationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	job, err := readCreatedJob(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"databricks-created-job": job}}, nil
}

// dex:field attribute-key:databricks-created-job value-type:json editable:false description:"Created job identity"
// GetDexDisplay shows the confirmed job ID and name.
func (*JobCreationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	job, err := readCreatedJob(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"databricks-created-job": job}}, nil
}

func readCreatedJob(ctx dex.Context) (databricks.JobSummary, error) {
	job, err := createdJobAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return databricks.JobSummary{}, nil
	}
	return job, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the explicit job settings to the provider Step."
type startJobCreation struct{ dex.StepDefaults }

func (startJobCreation) GetStepType() string { return "StartJobCreation" }
func (startJobCreation) WaitFor(dex.Context, databricks.CreateJobInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startJobCreation) Execute(_ dex.Context, input databricks.CreateJobInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[databricks.CreateJobInput]("CallCreateJob"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the confirmed job identity and complete the Flow."
type completeJobCreation struct {
	dex.StepDefaultsNoWaitFor[databricks.CreateJobResult]
}

func (completeJobCreation) GetStepType() string { return "CompleteJobCreation" }
func (completeJobCreation) Execute(ctx dex.Context, result databricks.CreateJobResult) (*dex.StepDecision, error) {
	if err := createdJobAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*JobCreationFlow)(nil)
