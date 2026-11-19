// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ashby/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createJobStepType = "CallCreateJob"

var createdJobAttribute = dex.DefineAttribute[ashby.JobSummary]("ashby-created-job")

// JobCreateFlow creates an Ashby job.
type JobCreateFlow struct {
	dex.FlowDefaults
	connection ashby.Connection
}

// NewJobCreateFlow binds the provider connection at registration time.
func NewJobCreateFlow(connection ashby.Connection) *JobCreateFlow {
	return &JobCreateFlow{connection: connection}
}

// GetFlowType returns the stable example identity.
func (*JobCreateFlow) GetFlowType() string { return "AshbyJobCreate" }

// GetPersistenceSchema registers the retained job result.
func (*JobCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdJobAttribute}}
}

// GetSteps registers the start, create Mutation, and completion Steps.
func (flow *JobCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(createJobStart{}),
		dex.DefineStep(ashby.NewCreateJobStep(ashby.CreateJobStepConfig[ashby.CreateJobInput]{
			StepType: createJobStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ashby", Explanation: "Create a job in Ashby."},
			Connection:  flow.connection, MapToOperationInput: mapToCreateJobInput,
			Created: sdkgo.GoTo(completeJobCreate{}),
		})),
		dex.DefineStep(completeJobCreate{}),
	}
}

// GetRPCs exposes read-only Dex Web views.
func (flow *JobCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ashby-created-job value-type:json editable:false description:"Created job"
// GetDexSummary shows the created job in Dex Web.
func (*JobCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	job, err := readCreatedJob(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-created-job": job}}, nil
}

// dex:field attribute-key:ashby-created-job value-type:json editable:false description:"Created job"
// GetDexDisplay shows the created job in Dex Web.
func (*JobCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	job, err := readCreatedJob(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-created-job": job}}, nil
}

func readCreatedJob(ctx dex.Context) (ashby.JobSummary, error) {
	job, err := createdJobAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ashby.JobSummary{}, nil
	}
	return job, err
}

func mapToCreateJobInput(input ashby.CreateJobInput) ashby.CreateJobInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the title, team, and location to the create Step."
type createJobStart struct{ dex.StepDefaults }

func (createJobStart) GetStepType() string { return "StartCreateJob" }
func (createJobStart) WaitFor(dex.Context, ashby.CreateJobInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (createJobStart) Execute(_ dex.Context, input ashby.CreateJobInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ashby.CreateJobInput](createJobStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created job and complete the Flow."
type completeJobCreate struct {
	dex.StepDefaultsNoWaitFor[ashby.CreateJobResult]
}

func (completeJobCreate) GetStepType() string { return "CompleteCreateJob" }
func (completeJobCreate) Execute(ctx dex.Context, result ashby.CreateJobResult) (*dex.StepDecision, error) {
	if err := createdJobAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*JobCreateFlow)(nil)
