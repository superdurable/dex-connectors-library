// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package job_runinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/databricks/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var jobPageAttribute = dex.DefineAttribute[databricks.JobPage]("databricks-job-page")

// JobListingFlowType is the stable Flow identity for listing Databricks jobs.
const JobListingFlowType = "DatabricksJobListing"

// JobListingFlow lists one page of saved jobs through the generated Connector Step.
type JobListingFlow struct {
	dex.FlowDefaults
	connection databricks.Connection
}

// NewJobListingFlow binds a Databricks connection for the job listing example.
func NewJobListingFlow(connection databricks.Connection) *JobListingFlow {
	return &JobListingFlow{connection: connection}
}

func (*JobListingFlow) GetFlowType() string { return JobListingFlowType }

func (*JobListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{jobPageAttribute}}
}

func (flow *JobListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:databricks-job-page value-type:json editable:false description:"Listed jobs"
// GetDexSummary shows the last job page in Dex Web.
func (*JobListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readJobPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"databricks-job-page": page}}, nil
}

// dex:field attribute-key:databricks-job-page value-type:json editable:false description:"Listed jobs"
// GetDexDisplay shows the last job page in Dex Web.
func (*JobListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readJobPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"databricks-job-page": page}}, nil
}

func readJobPage(ctx dex.Context) (databricks.JobPage, error) {
	page, err := jobPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return databricks.JobPage{}, nil
	}
	return page, err
}

func (flow *JobListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startJobListing{}),
		dex.DefineStep(databricks.NewListJobsStep(databricks.ListJobsStepConfig[databricks.ListJobsInput]{
			StepType: "CallListJobs", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Databricks", Explanation: "List one page of jobs from Databricks."},
			Connection:  flow.connection, MapToOperationInput: mapJobListingInput,
			Listed: sdkgo.GoTo(completeJobListing{}),
		})),
		dex.DefineStep(completeJobListing{}),
	}
}

func mapJobListingInput(input databricks.ListJobsInput) databricks.ListJobsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the job listing request to the provider Step."
type startJobListing struct{ dex.StepDefaults }

func (startJobListing) GetStepType() string { return "StartJobListing" }
func (startJobListing) WaitFor(dex.Context, databricks.ListJobsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startJobListing) Execute(_ dex.Context, input databricks.ListJobsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[databricks.ListJobsInput]("CallListJobs"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one page of Databricks jobs."
type completeJobListing struct {
	dex.StepDefaultsNoWaitFor[databricks.ListJobsResult]
}

func (completeJobListing) GetStepType() string { return "CompleteJobListing" }
func (completeJobListing) Execute(ctx dex.Context, result databricks.ListJobsResult) (*dex.StepDecision, error) {
	if err := jobPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*JobListingFlow)(nil)
