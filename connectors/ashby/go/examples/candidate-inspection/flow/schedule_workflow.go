// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ashby/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createInterviewScheduleStepType = "CallCreateInterviewSchedule"

var createdInterviewScheduleAttribute = dex.DefineAttribute[ashby.InterviewScheduleSummary]("ashby-created-schedule")

// InterviewScheduleCreateFlow creates an Ashby schedule.
type InterviewScheduleCreateFlow struct {
	dex.FlowDefaults
	connection ashby.Connection
}

// NewInterviewScheduleCreateFlow binds the provider connection at registration time.
func NewInterviewScheduleCreateFlow(connection ashby.Connection) *InterviewScheduleCreateFlow {
	return &InterviewScheduleCreateFlow{connection: connection}
}

// GetFlowType returns the stable example identity.
func (*InterviewScheduleCreateFlow) GetFlowType() string { return "AshbyInterviewScheduleCreate" }

// GetPersistenceSchema registers the retained schedule result.
func (*InterviewScheduleCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdInterviewScheduleAttribute}}
}

// GetSteps registers the start, create Mutation, and completion Steps.
func (flow *InterviewScheduleCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(createInterviewScheduleStart{}),
		dex.DefineStep(ashby.NewCreateInterviewScheduleStep(ashby.CreateInterviewScheduleStepConfig[ashby.CreateInterviewScheduleInput]{
			StepType: createInterviewScheduleStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ashby", Explanation: "Create a schedule in Ashby."},
			Connection:  flow.connection, MapToOperationInput: mapToCreateInterviewScheduleInput,
			Created: sdkgo.GoTo(completeInterviewScheduleCreate{}),
		})),
		dex.DefineStep(completeInterviewScheduleCreate{}),
	}
}

// GetRPCs exposes read-only Dex Web views.
func (flow *InterviewScheduleCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ashby-created-schedule value-type:json editable:false description:"Created schedule"
// GetDexSummary shows the created schedule in Dex Web.
func (*InterviewScheduleCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	schedule, err := readCreatedInterviewSchedule(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-created-schedule": schedule}}, nil
}

// dex:field attribute-key:ashby-created-schedule value-type:json editable:false description:"Created schedule"
// GetDexDisplay shows the created schedule in Dex Web.
func (*InterviewScheduleCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	schedule, err := readCreatedInterviewSchedule(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-created-schedule": schedule}}, nil
}

func readCreatedInterviewSchedule(ctx dex.Context) (ashby.InterviewScheduleSummary, error) {
	schedule, err := createdInterviewScheduleAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ashby.InterviewScheduleSummary{}, nil
	}
	return schedule, err
}

func mapToCreateInterviewScheduleInput(input ashby.CreateInterviewScheduleInput) ashby.CreateInterviewScheduleInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the application and event times to the schedule Step."
type createInterviewScheduleStart struct{ dex.StepDefaults }

func (createInterviewScheduleStart) GetStepType() string { return "StartCreateInterviewSchedule" }
func (createInterviewScheduleStart) WaitFor(dex.Context, ashby.CreateInterviewScheduleInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (createInterviewScheduleStart) Execute(_ dex.Context, input ashby.CreateInterviewScheduleInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ashby.CreateInterviewScheduleInput](createInterviewScheduleStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created schedule and complete the Flow."
type completeInterviewScheduleCreate struct {
	dex.StepDefaultsNoWaitFor[ashby.CreateInterviewScheduleResult]
}

func (completeInterviewScheduleCreate) GetStepType() string { return "CompleteCreateInterviewSchedule" }
func (completeInterviewScheduleCreate) Execute(ctx dex.Context, result ashby.CreateInterviewScheduleResult) (*dex.StepDecision, error) {
	if err := createdInterviewScheduleAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*InterviewScheduleCreateFlow)(nil)
