// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package employeechanged demonstrates the BambooHR employeeChanged Trigger: every employee BambooHR
// lists as inserted, updated, or deleted starts one Flow, which reads the employee back and records the
// fields an HR-to-IT hand-off needs.
package employeechanged

import (
	"errors"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/bamboohr/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each employee change starts.
	FlowType = "BambooHREmployeeChange"
	// ConnectionName is the static Dex Web connection, shared with the other BambooHR examples.
	ConnectionName = "bamboohr-company"
	// EmployeeChangedTriggerBinding is the stable employeeChanged binding that starts this Flow.
	EmployeeChangedTriggerBinding = "employee-changes"
	// FlowIDPrefix precedes the Trigger event ID in every Flow ID, so a redelivered change maps to one Flow.
	FlowIDPrefix = "bamboohr-employee-change-"

	recordEmployeeChangeStepType   = "RecordEmployeeChange"
	readChangedEmployeeStepType    = "ReadChangedEmployee"
	completeEmployeeChangeStepType = "CompleteEmployeeChange"
)

// HandOffFields are the employee fields the Flow reads back; terminationDate is set for a terminated
// employee, whose change BambooHR reports as Updated.
var HandOffFields = []string{"firstName", "lastName", "workEmail", "status", "department", "jobTitle", "terminationDate"}

var (
	employeeChangeAttribute        = dex.DefineAttribute[Input]("bamboohr-employee-change")
	employeeChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("bamboohr-employee-change-outcome")
)

// Input is one employeeChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change bamboohr.EmployeeChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// EmployeeID is the changed employee.
	EmployeeID string `json:"employeeId"`
	// Action is BambooHR's change action.
	Action bamboohr.EmployeeChangeAction `json:"action"`
	// LastChanged is when BambooHR recorded the change.
	LastChanged time.Time `json:"lastChanged"`
	// Current is the employee read back from BambooHR; nil for a deleted employee, which is not read, or
	// one deleted before the read.
	Current *bamboohr.EmployeeRecord `json:"current,omitempty"`
	// IsGone reports that BambooHR no longer has the employee.
	IsGone bool `json:"isGone,omitempty"`
}

// Flow records one BambooHR employee change.
type Flow struct {
	dex.FlowDefaults
	connection bamboohr.Connection
}

// NewFlow binds the BambooHR Connection at registration time.
func NewFlow(connection bamboohr.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and BambooHR connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordEmployeeChange{}),
		dex.DefineStep(bamboohr.NewGetEmployeeStep(bamboohr.GetEmployeeStepConfig[Input]{
			StepType: readChangedEmployeeStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed employee's hand-off fields back from BambooHR, since the change carries no field values.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetEmployeeInput,
			Found: sdkgo.GoTo(completeEmployeeChange{}),
			// Business reaction: an employee deleted before the read is recorded as gone; its Deleted change
			// starts its own Flow.
			NotFound: sdkgo.GoTo(completeEmployeeChange{}),
		})),
		dex.DefineStep(completeEmployeeChange{}),
	}
}

// GetConnectorTriggerBindings declares the employeeChanged binding and its change type unit for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		bamboohr.DefineEmployeeChangedTriggerBinding(bamboohr.EmployeeChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: EmployeeChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "changeType", UnitID: bamboohr.UIUnitChangeTypePicker, Label: "Employee changes",
				Description: "Choose which BambooHR employee changes start this Flow: Inserted for new hires, Updated for record, job, compensation, and employment status changes including terminations, Deleted for removed records, or Every change. The unit saves inserted, updated, or deleted; leaving it unsaved or choosing Every change starts a Flow for every change. The changes come from the employees the connection's API key user can see, so no BambooHR page needs a value. The first poll starts at the current time and earlier changes never start Flows; restart the Worker after saving, and a changed choice starts again from the current time.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: bamboohr.UIChangeTypePickerPortChangeType, JSONPointer: "/changeType"}},
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{employeeChangeAttribute, employeeChangeOutcomeAttribute}}
}

// GetDexSummary returns the delivered change and its outcome.
//
// dex:field attribute-key:bamboohr-employee-change value-type:json editable:false description:"Delivered employee change"
// dex:field attribute-key:bamboohr-employee-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := employeeChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"bamboohr-employee-change":         change,
		"bamboohr-employee-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the employee read back from BambooHR.
//
// dex:field attribute-key:bamboohr-employee-change value-type:json editable:false description:"Employee ID, action, and change time"
// dex:field attribute-key:bamboohr-employee-change-outcome value-type:json editable:false description:"Hand-off fields read back from BambooHR"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := employeeChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"bamboohr-employee-change":         change,
		"bamboohr-employee-change-outcome": outcome,
	}}, nil
}

func employeeChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, employeeChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, employeeChangeOutcomeAttribute)
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

// AcceptEveryEmployeeChange admits every delivered change; the binding's change type already filters
// them, and an application may narrow further, for example to one department after the read.
func AcceptEveryEmployeeChange(sdkgo.TriggerEvent[bamboohr.EmployeeChangedEvent]) bool { return true }

// ResolveFlowID names one Flow per change; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[bamboohr.EmployeeChangedEvent]) string {
	return FlowIDPrefix + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[bamboohr.EmployeeChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// MapToGetEmployeeInput reads the hand-off fields of the changed employee.
func MapToGetEmployeeInput(input Input) bamboohr.GetEmployeeInput {
	return bamboohr.GetEmployeeInput{EmployeeID: input.Change.EmployeeID, Fields: HandOffFields}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered change, completing at once for a deleted employee and otherwise reading the employee back."
type recordEmployeeChange struct {
	dex.StepDefaults
}

func (recordEmployeeChange) GetStepType() string { return recordEmployeeChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordEmployeeChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordEmployeeChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := employeeChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	if input.Change.Action == bamboohr.EmployeeChangeActionDeleted {
		outcome := ChangeOutcome{EmployeeID: input.Change.EmployeeID, Action: input.Change.Action, LastChanged: input.Change.LastChanged, IsGone: true}
		if err := employeeChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedEmployeeStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the employee read back from BambooHR, or that it is gone, as the outcome and complete the Flow."
type completeEmployeeChange struct {
	dex.StepDefaultsNoWaitFor[bamboohr.GetEmployeeResult]
}

func (completeEmployeeChange) GetStepType() string { return completeEmployeeChangeStepType }

func (completeEmployeeChange) Execute(ctx dex.Context, result bamboohr.GetEmployeeResult) (*dex.StepDecision, error) {
	change, err := employeeChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{EmployeeID: change.Change.EmployeeID, Action: change.Change.Action, LastChanged: change.Change.LastChanged}
	if result.Branch == bamboohr.GetEmployeeBranchFound {
		current := result.Value
		outcome.Current = &current
	} else {
		outcome.IsGone = true
	}
	if err := employeeChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.EmployeeID) == "" || input.Change.LastChanged.IsZero():
		return errors.New("change.employeeId and change.lastChanged are required")
	}
	switch input.Change.Action {
	case bamboohr.EmployeeChangeActionInserted, bamboohr.EmployeeChangeActionUpdated, bamboohr.EmployeeChangeActionDeleted:
		return nil
	default:
		return errors.New("change.action must be Inserted, Updated, or Deleted")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
