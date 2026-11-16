// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordchangedintake

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func changedRecordEvent() sdkgo.TriggerEvent[airtable.RecordChangedEvent] {
	modifiedAt := time.Date(2026, 11, 15, 9, 0, 0, 123000000, time.UTC)
	return sdkgo.TriggerEvent[airtable.RecordChangedEvent]{
		ID: "recLeadAda0000001-20261115T090000123000000Z", OccurredAt: modifiedAt,
		Payload: airtable.RecordChangedEvent{
			BaseID: "appIntakeBase0001", TableID: "tblIntakeLeads001", LastModifiedTime: modifiedAt, IsCreatedRecord: true,
			Record: airtable.Record{ID: "recLeadAda0000001", CreatedTime: modifiedAt, Fields: map[string]airtable.CellValue{
				"Email": airtable.TextCellValue("ada@example.test"),
			}},
		},
	}
}

func TestResolveFlowIDAndMapToFlowInputAreDeterministic(t *testing.T) {
	event := changedRecordEvent()
	require.Equal(t, "airtable-record-change-recLeadAda0000001-20261115T090000123000000Z", ResolveFlowID(event))
	require.Equal(t, ResolveFlowID(event), ResolveFlowID(changedRecordEvent()), "a redelivered change maps to the same Flow")
	require.True(t, AcceptChangedRecord(event))
	require.Equal(t, ChangedRecord{
		EventID: event.ID, BaseID: "appIntakeBase0001", TableID: "tblIntakeLeads001", RecordID: "recLeadAda0000001",
		IsCreatedRecord: true, LastModifiedTime: event.OccurredAt, Fields: event.Payload.Record.Fields,
	}, MapToFlowInput(event))
}

func TestFlowDeclaresItsRecordChangedBindingWithBaseTableAndFieldPickers(t *testing.T) {
	bindings := NewFlow(airtable.Connection{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	require.Equal(t, RecordChangedTriggerBinding, bindings[0].BindingName)
	require.Equal(t, ConnectionName, bindings[0].ConnectionName)
	require.Equal(t, airtable.RecordChangedTriggerDefinition, bindings[0].Definition)
	units := bindings[0].ConfigurationUI.Units
	require.Len(t, units, 3, "the binding shows exactly the base, table, and field pickers")
	require.Equal(t, []string{airtable.UIUnitBasePicker, airtable.UIUnitTablePicker, airtable.UIUnitLastModifiedTimeFieldPicker},
		[]string{units[0].UnitID, units[1].UnitID, units[2].UnitID})
	for _, unit := range units {
		require.True(t, unit.Required, unit.ID)
		require.Contains(t, unit.Description, "stores", unit.ID)
		require.Contains(t, unit.Description, "It cannot be blank.", unit.ID)
	}
	require.Contains(t, units[0].Description, "https://airtable.com/")
	require.Contains(t, units[1].Description, "never start Flows")
	require.Contains(t, units[2].Description, "Include time")
	require.Equal(t, []sdkgo.ConnectorUIBinding{
		{Port: airtable.UILastModifiedTimeFieldPickerPortBaseID, JSONPointer: "/baseId"},
		{Port: airtable.UILastModifiedTimeFieldPickerPortTableID, JSONPointer: "/tableId"},
		{Port: airtable.UILastModifiedTimeFieldPickerPortFieldID, JSONPointer: "/fieldId"},
		{Port: airtable.UILastModifiedTimeFieldPickerPortFieldName, JSONPointer: "/fieldName"},
	}, units[2].Bindings)

	// Every picker output decodes into the Trigger configuration the runner loads.
	var configuration airtable.RecordChangedTriggerConfiguration
	decoder := json.NewDecoder(strings.NewReader(`{"baseId":"appIntakeBase0001","baseName":"Intake","tableId":"tblIntakeLeads001","tableName":"Leads","fieldId":"fldLastModified01","fieldName":"Last Modified"}`))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&configuration))
	require.NoError(t, configuration.Validate())
	require.Equal(t, "fldLastModified01", configuration.LastModifiedTimeFieldID)
}

func TestFlowIdentitiesAndSchema(t *testing.T) {
	flow := NewFlow(airtable.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordChangeStepType, dex.GetFinalStepType[ChangedRecord](recordChange{}))
	require.Equal(t, completeIntakeStepType, dex.GetFinalStepType[airtable.GetRecordResult](completeIntake{}))
	require.Equal(t, recordRemovedRecordStepType, dex.GetFinalStepType[airtable.GetRecordResult](recordRemovedRecord{}))
	require.Equal(t, []dex.AttributeDef{intakeAttribute}, flow.GetPersistenceSchema().Attributes)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := airtable.New(airtable.Config{}, sdkgo.StaticCredentialProvider[airtable.Credentials]{})
	require.NoError(t, err)
	connection, err := airtable.NewConnection(client, sdkgo.ConnectionRef{Provider: "airtable", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)

	otherConnection, err := airtable.NewConnection(client, sdkgo.ConnectionRef{Provider: "airtable", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) })
}

func TestReadBackTargetsTheChangedRecordAndCompletesWithItsCurrentFields(t *testing.T) {
	change := MapToFlowInput(changedRecordEvent())
	require.Equal(t, airtable.GetRecordInput{
		BaseID: "appIntakeBase0001", TableIDOrName: "tblIntakeLeads001", RecordID: "recLeadAda0000001",
	}, MapToReadBackInput(change))

	current := map[string]airtable.CellValue{"Email": airtable.TextCellValue("ada@example.test"), "Status": airtable.TextCellValue("Qualified")}
	completed, err := CompleteWithCurrentRecord(Intake{Change: change}, airtable.Record{ID: change.RecordID, Fields: current})
	require.NoError(t, err)
	require.Equal(t, Intake{Change: change, CurrentFields: current, IsCompleted: true}, completed)

	_, err = CompleteWithCurrentRecord(Intake{Change: change}, airtable.Record{ID: "recSomeoneElse001"})
	require.ErrorContains(t, err, "instead of the changed record")
}
