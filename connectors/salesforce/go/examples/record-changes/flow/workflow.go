// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package recordchanges demonstrates the Salesforce recordModifiedPolled Trigger: every record of the
// picked object that is created or modified starts one Flow, which reads the record back and records its
// current state, or records that it was deleted before the read.
package recordchanges

import (
	"errors"
	"sort"
	"strings"

	salesforce "github.com/superdurable/dex-connectors-library/connectors/salesforce/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each record change starts.
	FlowType = "SalesforceRecordChange"
	// ConnectionName is the static Dex Web connection for Salesforce, shared with the record-sync example.
	ConnectionName = "salesforce-crm"
	// RecordModifiedTriggerBinding is the stable recordModifiedPolled binding that starts this Flow.
	RecordModifiedTriggerBinding = "record-changes"

	recordRecordChangeStepType   = "RecordRecordChange"
	readChangedRecordStepType    = "ReadChangedRecord"
	completeRecordChangeStepType = "CompleteRecordChange"
	completeDeletedRecordType    = "CompleteDeletedRecord"
)

var (
	recordChangeAttribute        = dex.DefineAttribute[Input]("salesforce-record-change")
	recordChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("salesforce-record-change-outcome")
)

// Input is one recordModifiedPolled delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered record change.
	Change salesforce.RecordModifiedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// SObjectType is the watched object.
	SObjectType string `json:"sObjectType"`
	// RecordID is the changed record.
	RecordID string `json:"recordId"`
	// ChangeKind is the delivered change kind.
	ChangeKind salesforce.RecordChangeKind `json:"changeKind"`
	// IsDeleted reports that the record was deleted before the Flow read it back.
	IsDeleted bool `json:"isDeleted"`
	// Current is the record read back from Salesforce; nil when IsDeleted is true.
	Current *salesforce.Record `json:"current,omitempty"`
}

// Flow records one Salesforce record change.
type Flow struct {
	dex.FlowDefaults
	connection salesforce.Connection
}

// NewFlow binds the Salesforce Connection at registration time.
func NewFlow(connection salesforce.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Salesforce connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordRecordChange{}),
		dex.DefineStep(salesforce.NewGetRecordStep(salesforce.GetRecordStepConfig[Input]{
			StepType: readChangedRecordStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed record back, so the outcome holds its current values rather than the polled snapshot.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetRecordInput,
			Found: sdkgo.GoTo(completeRecordChange{}),
			// Business reaction: a record deleted after the poll read it completes as deleted instead of failing.
			NotFound: sdkgo.GoTo(completeDeletedRecord{}),
		})),
		dex.DefineStep(completeRecordChange{}),
		dex.DefineStep(completeDeletedRecord{}),
	}
}

// GetConnectorTriggerBindings declares the recordModifiedPolled binding with its object and field units.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		salesforce.DefineRecordModifiedPolledTriggerBinding(salesforce.RecordModifiedPolledTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: RecordModifiedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "watchedObject", UnitID: salesforce.UIUnitSObjectPicker, Label: "Watched object", Required: true,
					Description: "Choose the object whose created and modified records start this Flow, such as Opportunity, or sign in at https://login.salesforce.com (https://test.salesforce.com for a sandbox) and enter a custom object API name from Setup > Object Manager > the object > API Name, such as Invoice__c; the unit saves the non-secret object API name, and it is required. The first start records existing records as a baseline and starts no Flow for them. Restart the Worker after saving; a changed object records a new baseline.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: salesforce.UISObjectPickerPortSObjectType, JSONPointer: "/sObjectType"}},
				},
				{
					ID: "deliveredFields", UnitID: salesforce.UIUnitFieldNamesInput, Label: "Delivered fields",
					Description: "Enter the comma-separated field API names each delivered change carries and the Flow reads back, such as Name, StageName, after signing in at https://login.salesforce.com (https://test.salesforce.com for a sandbox) under Setup > Object Manager > the object > Fields & Relationships > Field Name column; the unit saves the non-secret list. Blank delivers only Id, CreatedDate, LastModifiedDate, and SystemModstamp. Restart the Worker after saving; the cursor is kept.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: salesforce.UIFieldNamesInputPortFieldNames, JSONPointer: "/fieldNames"}},
				},
			}},
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{recordChangeAttribute, recordChangeOutcomeAttribute}}
}

// GetDexSummary returns the change and outcome.
//
// dex:field attribute-key:salesforce-record-change value-type:json editable:false description:"Delivered record change"
// dex:field attribute-key:salesforce-record-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := recordChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"salesforce-record-change":         change,
		"salesforce-record-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the record read back from Salesforce.
//
// dex:field attribute-key:salesforce-record-change value-type:json editable:false description:"Object, change kind, and record from the poll"
// dex:field attribute-key:salesforce-record-change-outcome value-type:json editable:false description:"Current record read back from Salesforce"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := recordChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"salesforce-record-change":         change,
		"salesforce-record-change-outcome": outcome,
	}}, nil
}

func recordChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, recordChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, recordChangeOutcomeAttribute)
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

// ResolveFlowID names one Flow per change; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[salesforce.RecordModifiedEvent]) string {
	return "salesforce-record-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[salesforce.RecordModifiedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryRecordChange admits every change; an application narrows it, for example to created records.
func AcceptEveryRecordChange(sdkgo.TriggerEvent[salesforce.RecordModifiedEvent]) bool { return true }

// MapToGetRecordInput reads back every field the delivered change carries, in a stable order.
func MapToGetRecordInput(input Input) salesforce.GetRecordInput {
	fields := make([]string, 0, len(input.Change.Record.Fields))
	for name := range input.Change.Record.Fields {
		fields = append(fields, name)
	}
	sort.Strings(fields)
	return salesforce.GetRecordInput{SObjectType: input.Change.SObjectType, RecordID: input.Change.RecordID, Fields: fields}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered record change, then read the record back from Salesforce."
type recordRecordChange struct {
	dex.StepDefaults
}

func (recordRecordChange) GetStepType() string { return recordRecordChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordRecordChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordRecordChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := recordChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedRecordStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the record read back from Salesforce as the outcome and complete the Flow."
type completeRecordChange struct {
	dex.StepDefaultsNoWaitFor[salesforce.GetRecordResult]
}

func (completeRecordChange) GetStepType() string { return completeRecordChangeStepType }

func (completeRecordChange) Execute(ctx dex.Context, result salesforce.GetRecordResult) (*dex.StepDecision, error) {
	current := result.Value
	outcome, err := recordOutcome(ctx, &current)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record that the record was deleted before the read-back and complete the Flow."
type completeDeletedRecord struct {
	dex.StepDefaultsNoWaitFor[salesforce.GetRecordResult]
}

func (completeDeletedRecord) GetStepType() string { return completeDeletedRecordType }

func (completeDeletedRecord) Execute(ctx dex.Context, _ salesforce.GetRecordResult) (*dex.StepDecision, error) {
	outcome, err := recordOutcome(ctx, nil)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// recordOutcome stores the outcome Attribute; a nil current record means it was deleted.
func recordOutcome(ctx dex.Context, current *salesforce.Record) (ChangeOutcome, error) {
	change, err := recordChangeAttribute.Get(ctx)
	if err != nil {
		return ChangeOutcome{}, err
	}
	outcome := ChangeOutcome{
		SObjectType: change.Change.SObjectType, RecordID: change.Change.RecordID, ChangeKind: change.Change.ChangeKind,
		IsDeleted: current == nil, Current: current,
	}
	return outcome, recordChangeOutcomeAttribute.Set(ctx, outcome)
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.SObjectType) == "" || strings.TrimSpace(input.Change.RecordID) == "":
		return errors.New("change.sObjectType and change.recordId are required")
	}
	switch input.Change.ChangeKind {
	case salesforce.RecordChangeCreated, salesforce.RecordChangeUpdated:
		return nil
	default:
		return errors.New("change.changeKind must be created or updated")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
