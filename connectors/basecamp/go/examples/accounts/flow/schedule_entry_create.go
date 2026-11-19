// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/basecamp/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ScheduleEntryCreateFlowType identifies the schedule entry creation example.
const ScheduleEntryCreateFlowType = "BasecampScheduleEntryCreate"

const scheduleEntryCreateStepType = "CallCreateScheduleEntry"

var createdScheduleEntryAttribute = dex.DefineAttribute[basecamp.ScheduleEntry]("basecamp-created-schedule-entry")

// ScheduleEntryCreateFlow creates one Basecamp schedule entry.
type ScheduleEntryCreateFlow struct {
	dex.FlowDefaults
	connection basecamp.Connection
}

// NewScheduleEntryCreateFlow binds the provider connection at registration.
func NewScheduleEntryCreateFlow(connection basecamp.Connection) *ScheduleEntryCreateFlow {
	return &ScheduleEntryCreateFlow{connection: connection}
}

func (*ScheduleEntryCreateFlow) GetFlowType() string { return ScheduleEntryCreateFlowType }

func (*ScheduleEntryCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdScheduleEntryAttribute}}
}

// GetRPCs registers read-only schedule entry views.
func (flow *ScheduleEntryCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:basecamp-created-schedule-entry value-type:json editable:false description:"Created schedule entry"
// GetDexSummary shows the created entry identity.
func (*ScheduleEntryCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	list, err := readCreatedScheduleEntry(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-created-schedule-entry": list}}, nil
}

// dex:field attribute-key:basecamp-created-schedule-entry value-type:json editable:false description:"Created schedule entry"
// GetDexDisplay shows the created entry identity.
func (*ScheduleEntryCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	list, err := readCreatedScheduleEntry(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-created-schedule-entry": list}}, nil
}

func readCreatedScheduleEntry(ctx dex.Context) (basecamp.ScheduleEntry, error) {
	list, err := createdScheduleEntryAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return basecamp.ScheduleEntry{}, nil
	}
	return list, err
}

func (flow *ScheduleEntryCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startScheduleEntryCreate{}),
		dex.DefineStep(basecamp.NewCreateScheduleEntryStep(basecamp.CreateScheduleEntryStepConfig[basecamp.CreateScheduleEntryInput]{
			StepType: scheduleEntryCreateStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Basecamp", Explanation: "Create one schedule entry in the selected schedule."},
			Connection:          flow.connection,
			MapToOperationInput: func(input basecamp.CreateScheduleEntryInput) basecamp.CreateScheduleEntryInput { return input },
			Created:             sdkgo.GoTo(completeScheduleEntryCreate{}),
		})),
		dex.DefineStep(completeScheduleEntryCreate{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the entry fields to the creation Step."
type startScheduleEntryCreate struct{ dex.StepDefaults }

func (startScheduleEntryCreate) GetStepType() string { return "StartCreateScheduleEntry" }
func (startScheduleEntryCreate) WaitFor(dex.Context, basecamp.CreateScheduleEntryInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startScheduleEntryCreate) Execute(_ dex.Context, input basecamp.CreateScheduleEntryInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[basecamp.CreateScheduleEntryInput](scheduleEntryCreateStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created entry identity."
type completeScheduleEntryCreate struct {
	dex.StepDefaultsNoWaitFor[basecamp.CreateScheduleEntryResult]
}

func (completeScheduleEntryCreate) GetStepType() string { return "CompleteCreateScheduleEntry" }
func (completeScheduleEntryCreate) Execute(ctx dex.Context, result basecamp.CreateScheduleEntryResult) (*dex.StepDecision, error) {
	if err := createdScheduleEntryAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ScheduleEntryCreateFlow)(nil)
