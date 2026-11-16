// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package recordchangedintake starts one Flow per record created or changed in a picked Airtable table,
// through the recordChanged Trigger, reads the record back from Airtable, and records both for review in Dex Web.
package recordchangedintake

import (
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/airtable/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity shown in Dex Web.
	FlowType = "AirtableRecordChangedIntake"
	// ConnectionName is the static Dex Web connection whose table the Trigger polls.
	ConnectionName = "airtable-intake"
	// RecordChangedTriggerBinding is the recordChanged binding whose changes start this Flow.
	RecordChangedTriggerBinding = "intake-record-changed"
	// FlowIDPrefix precedes the Trigger event ID in every Flow ID, so a redelivered change maps to one Flow.
	FlowIDPrefix = "airtable-record-change-"

	recordChangeStepType          = "RecordChangedRecord"
	readBackChangedRecordStepType = "ReadBackChangedRecord"
	completeIntakeStepType        = "CompleteRecordIntake"
	recordRemovedRecordStepType   = "RecordRemovedRecord"
)

var intakeAttribute = dex.DefineAttribute[Intake]("airtable-record-intake")

// ChangedRecord is the Flow's start input: one change the recordChanged Trigger found.
type ChangedRecord struct {
	// EventID is the Trigger event ID, such as recXXXXXXXXXXXXXX-20261115T090000123000000Z.
	EventID string `json:"eventId"`
	// BaseID is the polled base ID.
	BaseID string `json:"baseId"`
	// TableID is the polled table ID.
	TableID string `json:"tableId"`
	// RecordID is the changed record's ID.
	RecordID string `json:"recordId"`
	// IsCreatedRecord reports a record that was created and not edited since.
	IsCreatedRecord bool `json:"isCreatedRecord"`
	// LastModifiedTime is the record's Last modified time value.
	LastModifiedTime time.Time `json:"lastModifiedTime"`
	// Fields maps each non-empty field name to its cell value.
	Fields map[string]airtable.CellValue `json:"fields"`
}

// Intake is the Flow's state and output: the change the Trigger reported and the record as the Flow read it back.
type Intake struct {
	// Change is the change the Trigger reported.
	Change ChangedRecord `json:"change"`
	// CurrentFields maps each non-empty field name to its cell value when the Flow read the record back;
	// it is empty until then and when the record was deleted.
	CurrentFields map[string]airtable.CellValue `json:"currentFields,omitempty"`
	// IsRemoved reports a record deleted after the change and before the Flow read it back.
	IsRemoved bool `json:"isRemoved"`
	// IsCompleted reports that the Flow finished reading the record back.
	IsCompleted bool `json:"isCompleted"`
}

// Flow records one changed Airtable record and reads it back through the Airtable Connection.
type Flow struct {
	dex.FlowDefaults

	connection airtable.Connection
}

// NewFlow binds the Airtable Connection named ConnectionName, the same connection whose table the Trigger polls.
func NewFlow(connection airtable.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps records the change, reads the record back from Airtable, and completes with its current fields,
// or as removed when the record was deleted in the meantime.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordChange{}),
		dex.DefineStep(airtable.NewGetRecordStep(airtable.GetRecordStepConfig[ChangedRecord]{
			StepType: readBackChangedRecordStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "airtable", GroupLabel: "Airtable",
				Explanation: "Read the changed record back by its ID to capture its current fields.",
			},
			Connection: flow.connection, MapToOperationInput: MapToReadBackInput,
			Found:    sdkgo.GoTo(completeIntake{}),
			NotFound: sdkgo.GoTo(recordRemovedRecord{}),
		})),
		dex.DefineStep(completeIntake{}),
		dex.DefineStep(recordRemovedRecord{}),
	}
}

// GetRPCs returns the summary and display RPCs that Dex Web shows.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the intake Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{intakeAttribute}}
}

// GetConnectorTriggerBindings declares the recordChanged binding that starts this Flow, with pickers for its
// base, table, and Last modified time field.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		airtable.DefineRecordChangedTriggerBinding(airtable.RecordChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: RecordChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "intakeBase", UnitID: airtable.UIUnitBasePicker, Label: "Base", Required: true,
					Description: "Select the base that holds the table to watch; Load bases lists the bases the personal access token can reach, and the picker stores the base ID and name. To enter the ID instead, copy the app followed by 14 letters and digits from the base's https://airtable.com/ address. It cannot be blank.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: airtable.UIBasePickerPortBaseID, JSONPointer: "/baseId"},
						{Port: airtable.UIBasePickerPortBaseName, JSONPointer: "/baseName"},
					},
				},
				{
					ID: "intakeTable", UnitID: airtable.UIUnitTablePicker, Label: "Table", Required: true,
					Description: "Select the table whose created and changed records start this Flow, after saving the base; the picker stores the table ID, which keeps working after a rename, and the table name. Records already in the table when the Worker first polls never start Flows. It cannot be blank.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: airtable.UITablePickerPortBaseID, JSONPointer: "/baseId"},
						{Port: airtable.UITablePickerPortTableID, JSONPointer: "/tableId"},
						{Port: airtable.UITablePickerPortTableName, JSONPointer: "/tableName"},
					},
				},
				{
					ID: "intakeLastModified", UnitID: airtable.UIUnitLastModifiedTimeFieldPicker, Label: "Last modified time field", Required: true,
					Description: "Select the table's Last modified time field, after saving the table; the picker stores the field ID and name. If the table has none, add a field of type Last modified time with Include time turned on in Airtable. Its setting decides what counts as a change: all fields, or only the fields it watches, so a Status-only field starts a Flow only when Status changes. It cannot be blank.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: airtable.UILastModifiedTimeFieldPickerPortBaseID, JSONPointer: "/baseId"},
						{Port: airtable.UILastModifiedTimeFieldPickerPortTableID, JSONPointer: "/tableId"},
						{Port: airtable.UILastModifiedTimeFieldPickerPortFieldID, JSONPointer: "/fieldId"},
						{Port: airtable.UILastModifiedTimeFieldPickerPortFieldName, JSONPointer: "/fieldName"},
					},
				},
			}},
		}),
	}
}

// GetDexSummary returns the recorded change for the Dex Web run list.
//
// dex:field attribute-key:airtable-record-intake value-type:json editable:false description:"Changed Airtable record and its current fields"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	intake, err := optionalAttribute(ctx, intakeAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"airtable-record-intake": intake}}, nil
}

// GetDexDisplay returns the recorded change for the Dex Web run detail.
//
// dex:field attribute-key:airtable-record-intake value-type:json editable:false description:"Base, table, record ID, modification time, fields at the change, and current fields"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	intake, err := optionalAttribute(ctx, intakeAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"airtable-record-intake": intake}}, nil
}

// AcceptChangedRecord is the application's admission rule: every change the Trigger reports starts a Flow.
func AcceptChangedRecord(sdkgo.TriggerEvent[airtable.RecordChangedEvent]) bool { return true }

// ResolveFlowID derives the Flow ID from the event ID, so every redelivery of a change maps to one Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[airtable.RecordChangedEvent]) string {
	return FlowIDPrefix + event.ID
}

// MapToFlowInput copies the changed record into the Flow's start input.
func MapToFlowInput(event sdkgo.TriggerEvent[airtable.RecordChangedEvent]) ChangedRecord {
	change := event.Payload
	return ChangedRecord{
		EventID: event.ID, BaseID: change.BaseID, TableID: change.TableID, RecordID: change.Record.ID,
		IsCreatedRecord: change.IsCreatedRecord, LastModifiedTime: change.LastModifiedTime, Fields: change.Record.Fields,
	}
}

// MapToReadBackInput reads the changed record from its polled base and table.
func MapToReadBackInput(change ChangedRecord) airtable.GetRecordInput {
	return airtable.GetRecordInput{BaseID: change.BaseID, TableIDOrName: change.TableID, RecordID: change.RecordID}
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:intake group-label:"Intake"
// dex:explanation text:"Persist the changed Airtable record, then read it back from Airtable."
type recordChange struct {
	dex.StepDefaultsNoWaitFor[ChangedRecord]
}

func (recordChange) GetStepType() string { return recordChangeStepType }

func (recordChange) Execute(ctx dex.Context, change ChangedRecord) (*dex.StepDecision, error) {
	if change.EventID == "" || change.RecordID == "" || change.LastModifiedTime.IsZero() {
		return dex.ForceFail("a changed record requires its event ID, record ID, and modification time"), nil
	}
	if err := intakeAttribute.Set(ctx, Intake{Change: change}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ChangedRecord](readBackChangedRecordStepType), change), nil
}

// dex:group group-id:airtable group-label:"Airtable"
// dex:explanation text:"Store the record's current fields and complete the Flow."
type completeIntake struct {
	dex.StepDefaultsNoWaitFor[airtable.GetRecordResult]
}

func (completeIntake) GetStepType() string { return completeIntakeStepType }

func (completeIntake) Execute(ctx dex.Context, result airtable.GetRecordResult) (*dex.StepDecision, error) {
	intake, err := intakeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	completed, err := CompleteWithCurrentRecord(intake, result.Value)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := intakeAttribute.Set(ctx, completed); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(completed), nil
}

// dex:group group-id:airtable group-label:"Airtable"
// dex:explanation text:"Mark the record as deleted since the change and complete the Flow."
type recordRemovedRecord struct {
	dex.StepDefaultsNoWaitFor[airtable.GetRecordResult]
}

func (recordRemovedRecord) GetStepType() string { return recordRemovedRecordStepType }

func (recordRemovedRecord) Execute(ctx dex.Context, _ airtable.GetRecordResult) (*dex.StepDecision, error) {
	intake, err := intakeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	intake.IsRemoved, intake.IsCompleted = true, true
	if err := intakeAttribute.Set(ctx, intake); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(intake), nil
}

// CompleteWithCurrentRecord stores the read-back record's fields; the record must be the changed one.
func CompleteWithCurrentRecord(intake Intake, record airtable.Record) (Intake, error) {
	if record.ID != intake.Change.RecordID {
		return Intake{}, fmt.Errorf("Airtable returned record %s instead of the changed record %s", record.ID, intake.Change.RecordID)
	}
	intake.CurrentFields, intake.IsCompleted = record.Fields, true
	return intake, nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
