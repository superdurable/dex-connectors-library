// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package taskchanged demonstrates the Asana taskChanged Trigger: every task added to, changed in,
// removed from, deleted from, or restored to the picked project starts one Flow, which reads the task
// back and records its current state.
package taskchanged

import (
	"errors"
	"strings"

	asana "github.com/superdurable/dex-connectors-library/connectors/asana/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each task change starts.
	FlowType = "AsanaTaskChange"
	// ConnectionName is the static Dex Web connection for Asana.
	ConnectionName = "asana-task-watcher"
	// TaskChangedTriggerBinding is the stable taskChanged binding that starts this Flow.
	TaskChangedTriggerBinding = "project-task-changes"

	recordTaskChangeStepType   = "RecordTaskChange"
	readChangedTaskStepType    = "ReadChangedTask"
	completeTaskChangeStepType = "CompleteTaskChange"
)

var (
	taskChangeAttribute        = dex.DefineAttribute[Input]("asana-task-change")
	taskChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("asana-task-change-outcome")
)

// Input is one taskChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered task event.
	Change asana.TaskChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// ProjectID is the watched project.
	ProjectID string `json:"projectId"`
	// TaskID is the changed task.
	TaskID string `json:"taskId"`
	// Action is the delivered action.
	Action asana.TaskChangeAction `json:"action"`
	// ChangedField is the changed task field, set only for a changed action.
	ChangedField string `json:"changedField,omitempty"`
	// Current is the task read back from Asana; nil for a deleted task, which is not read.
	Current *asana.Task `json:"current,omitempty"`
}

// Flow records one Asana task change.
type Flow struct {
	dex.FlowDefaults
	connection asana.Connection
}

// NewFlow binds the Asana Connection at registration time.
func NewFlow(connection asana.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Asana connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordTaskChange{}),
		dex.DefineStep(asana.NewGetTaskStep(asana.GetTaskStepConfig[Input]{
			StepType: readChangedTaskStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed task back, because Asana events name the change but not the task's current state.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetTaskInput,
			Found: sdkgo.GoTo(completeTaskChange{}),
		})),
		dex.DefineStep(completeTaskChange{}),
	}
}

// GetConnectorTriggerBindings declares the taskChanged binding and its project picker for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		asana.DefineTaskChangedTriggerBinding(asana.TaskChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: TaskChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "watchedProject", UnitID: asana.UIUnitProjectPicker, Label: "Watched project", Required: true,
				Description: "Choose the workspace, then the Asana project whose tasks start this Flow when they are added, changed, removed, deleted, or restored, subtasks included; the lists come from this connection's Asana user and save the workspace ID, project ID, and project name. " +
					"Without the lists, enter the project gid, the long number after /project/ in the project's Asana web address. A project is required; there is no blank default. " +
					"The first start records a baseline and starts no Flow for existing tasks. Restart the Worker after saving; a changed project records a new baseline.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: asana.UIProjectPickerPortWorkspaceID, JSONPointer: "/workspaceId"},
					{Port: asana.UIProjectPickerPortProjectID, JSONPointer: "/projectId"},
					{Port: asana.UIProjectPickerPortProjectName, JSONPointer: "/projectName"},
				},
			}}},
		}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the change and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{taskChangeAttribute, taskChangeOutcomeAttribute}}
}

// GetDexSummary returns the change and outcome.
//
// dex:field attribute-key:asana-task-change value-type:json editable:false description:"Delivered task change"
// dex:field attribute-key:asana-task-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := taskChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"asana-task-change":         change,
		"asana-task-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the task read back from Asana.
//
// dex:field attribute-key:asana-task-change value-type:json editable:false description:"Project, action, task, and changed field from the Asana event"
// dex:field attribute-key:asana-task-change-outcome value-type:json editable:false description:"Current task read back from Asana"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := taskChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"asana-task-change":         change,
		"asana-task-change-outcome": outcome,
	}}, nil
}

func taskChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, taskChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, taskChangeOutcomeAttribute)
	return change, outcome, err
}

// optionalAttribute returns the zero value while a running Flow has not set the Attribute yet.
func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// ResolveFlowID names one Flow per task event; a redelivered event finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[asana.TaskChangedEvent]) string {
	return "asana-task-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[asana.TaskChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryTaskChange admits every task event; an application narrows it, for example to completions.
func AcceptEveryTaskChange(sdkgo.TriggerEvent[asana.TaskChangedEvent]) bool { return true }

// MapToGetTaskInput reads the changed task.
func MapToGetTaskInput(input Input) asana.GetTaskInput {
	return asana.GetTaskInput{TaskID: input.Change.Task.ID}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered task event, completing at once for a deleted task and otherwise reading the task back."
type recordTaskChange struct {
	dex.StepDefaults
}

func (recordTaskChange) GetStepType() string { return recordTaskChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordTaskChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordTaskChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := taskChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	if input.Change.Action == asana.TaskChangeDeleted {
		outcome := newChangeOutcome(input.Change, nil)
		if err := taskChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedTaskStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the task read back from Asana as the outcome and complete the Flow."
type completeTaskChange struct {
	dex.StepDefaultsNoWaitFor[asana.GetTaskResult]
}

func (completeTaskChange) GetStepType() string { return completeTaskChangeStepType }

func (completeTaskChange) Execute(ctx dex.Context, result asana.GetTaskResult) (*dex.StepDecision, error) {
	change, err := taskChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	current := result.Value
	outcome := newChangeOutcome(change.Change, &current)
	if err := taskChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func newChangeOutcome(change asana.TaskChangedEvent, current *asana.Task) ChangeOutcome {
	return ChangeOutcome{ProjectID: change.ProjectID, TaskID: change.Task.ID, Action: change.Action, ChangedField: change.ChangedField, Current: current}
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.ProjectID) == "" || strings.TrimSpace(input.Change.Task.ID) == "":
		return errors.New("change.projectId and change.task.id are required")
	}
	switch input.Change.Action {
	case asana.TaskChangeAdded, asana.TaskChangeChanged, asana.TaskChangeRemoved, asana.TaskChangeDeleted, asana.TaskChangeUndeleted:
		return nil
	default:
		return errors.New("change.action must be added, changed, removed, deleted, or undeleted")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
