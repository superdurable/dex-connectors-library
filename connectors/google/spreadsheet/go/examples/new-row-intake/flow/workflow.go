// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package newrowintake starts one Flow per row appended to a picked Google Sheets tab, through the
// rowAdded Trigger, reads the row back from Google Sheets, and records both for review in Dex Web.
package newrowintake

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity shown in Dex Web.
	FlowType = "GoogleSheetsNewRowIntake"
	// ConnectionName is the static Dex Web connection whose tab the Trigger polls.
	ConnectionName = "google-sheets-intake"
	// NewRowTriggerBinding is the rowAdded binding whose rows start this Flow.
	NewRowTriggerBinding = "new-intake-row"
	// FlowIDPrefix precedes the Trigger event ID in every Flow ID, so a redelivered row maps to one Flow.
	FlowIDPrefix = "google-sheets-row-"

	recordAddedRowStepType    = "RecordAddedRow"
	readAddedRowStepType      = "ReadAddedRow"
	completeRowIntakeStepType = "CompleteRowIntake"
)

var addedRowAttribute = dex.DefineAttribute[Outcome]("google-sheets-added-row")

// AddedRow is the Flow's start input: one row the rowAdded Trigger found.
type AddedRow struct {
	// EventID is the Trigger event ID, such as row-12-0f3a9c1d2b4e5f60.
	EventID string `json:"eventId"`
	// SpreadsheetID is the polled spreadsheet ID.
	SpreadsheetID string `json:"spreadsheetId"`
	// SheetName is the polled tab title.
	SheetName string `json:"sheetName"`
	// RowNumber is the sheet row number when the row was read.
	RowNumber int64 `json:"rowNumber"`
	// Values maps the tab's header cells to the row's formatted cells.
	Values map[string]string `json:"values"`
	// DetectedAt is when the poll read the row.
	DetectedAt time.Time `json:"detectedAt"`
}

// Outcome is the durable result of one intake and the Flow's completion output.
type Outcome struct {
	// Row is the row the Trigger reported.
	Row AddedRow `json:"row"`
	// CurrentCells are the row's formatted cells read back from Google Sheets when the Flow ran; they
	// differ from Row when the row was edited, or rows above it moved, after the poll. Nil until read.
	CurrentCells []string `json:"currentCells,omitempty"`
}

// Flow records one appended spreadsheet row and reads it back through the getValues Step.
type Flow struct {
	dex.FlowDefaults
	connection spreadsheet.Connection
}

// NewFlow binds the Google Sheets Connection used by the read-back Step.
func NewFlow(connection spreadsheet.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the record, Google Sheets read, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordAddedRow{}),
		dex.DefineStep(spreadsheet.NewGetValuesStep(spreadsheet.GetValuesStepConfig[AddedRow]{
			StepType: readAddedRowStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-sheets", GroupLabel: "Google Sheets",
				Explanation: "Read the added row's current cells back from the tab.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetValuesInput,
			Read: sdkgo.GoTo(completeRowIntake{}),
		})),
		dex.DefineStep(completeRowIntake{}),
	}
}

// GetRPCs returns the summary and display RPCs that Dex Web shows.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the added-row Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{addedRowAttribute}}
}

// GetConnectorTriggerBindings declares the rowAdded binding that starts this Flow, with pickers for its
// spreadsheet and tab.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		spreadsheet.DefineRowAddedTriggerBinding(spreadsheet.RowAddedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: NewRowTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "spreadsheet", UnitID: spreadsheet.UIUnitSpreadsheetPicker, Label: "Spreadsheet", Required: true,
					Description: "Select the spreadsheet whose new rows start this Flow. Choose spreadsheet opens the Google Picker, which grants this app the drive.file access the Trigger polls with, and the unit stores the spreadsheet ID. To enter the ID instead, copy the non-secret text between /d/ and /edit in the spreadsheet's https://docs.google.com/spreadsheets/d/ address; it works only for a spreadsheet already granted to this app. It cannot be blank.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: spreadsheet.UISpreadsheetPickerPortSpreadsheetID, JSONPointer: "/spreadsheetId"}},
				},
				{
					ID: "tab", UnitID: spreadsheet.UIUnitSheetTabPicker, Label: "Tab", Required: true,
					Description: "Select the tab to watch; the picker loads the chosen spreadsheet's current tab titles and stores the exact title. Row 1 must hold the column headers. Rows already on the tab when the Worker first polls never start Flows, and renaming the tab later stops the Trigger until you pick it again. It cannot be blank.",
					Bindings: []sdkgo.ConnectorUIBinding{
						{Port: spreadsheet.UISheetTabPickerPortSpreadsheetID, JSONPointer: "/spreadsheetId"},
						{Port: spreadsheet.UISheetTabPickerPortTab, JSONPointer: "/sheetName"},
					},
				},
			}},
		}),
	}
}

// GetDexSummary returns the recorded row for the Dex Web run list.
//
// dex:field attribute-key:google-sheets-added-row value-type:json editable:false description:"Added spreadsheet row"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	row, err := optionalAttribute(ctx, addedRowAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-sheets-added-row": row}}, nil
}

// GetDexDisplay returns the recorded row for the Dex Web run detail.
//
// dex:field attribute-key:google-sheets-added-row value-type:json editable:false description:"Spreadsheet, tab, row number, header-keyed values, and the cells read back"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	row, err := optionalAttribute(ctx, addedRowAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-sheets-added-row": row}}, nil
}

// AcceptAddedRow is the application's admission rule: every row the Trigger reports starts a Flow.
func AcceptAddedRow(sdkgo.TriggerEvent[spreadsheet.RowAddedEvent]) bool { return true }

// ResolveFlowID derives the Flow ID from the event ID, so every redelivery of a row maps to one Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[spreadsheet.RowAddedEvent]) string {
	return FlowIDPrefix + event.ID
}

// MapToFlowInput copies the added row into the Flow's start input.
func MapToFlowInput(event sdkgo.TriggerEvent[spreadsheet.RowAddedEvent]) AddedRow {
	row := event.Payload
	return AddedRow{
		EventID: event.ID, SpreadsheetID: row.SpreadsheetID, SheetName: row.SheetName, RowNumber: row.RowNumber,
		Values: row.Values, DetectedAt: event.OccurredAt,
	}
}

// MapToGetValuesInput reads the reported row number of the polled tab as one whole-row A1 range, such as
// 'Leads'!7:7; a single quote in the tab title is doubled, as A1 notation requires.
func MapToGetValuesInput(row AddedRow) spreadsheet.GetValuesInput {
	rowNumber := strconv.FormatInt(row.RowNumber, 10)
	return spreadsheet.GetValuesInput{
		SpreadsheetID: row.SpreadsheetID,
		Range:         "'" + strings.ReplaceAll(row.SheetName, "'", "''") + "'!" + rowNumber + ":" + rowNumber,
	}
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
// dex:explanation text:"Persist the added spreadsheet row the rowAdded Trigger reported."
type recordAddedRow struct {
	dex.StepDefaultsNoWaitFor[AddedRow]
}

func (recordAddedRow) GetStepType() string { return recordAddedRowStepType }

func (recordAddedRow) Execute(ctx dex.Context, row AddedRow) (*dex.StepDecision, error) {
	if row.EventID == "" || row.SpreadsheetID == "" || row.RowNumber < 2 {
		return dex.ForceFail("an added row requires its event ID, spreadsheet ID, and a row number below the header"), nil
	}
	if err := addedRowAttribute.Set(ctx, Outcome{Row: row}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[AddedRow](readAddedRowStepType), row), nil
}

// dex:group group-id:intake-complete group-label:"Complete intake"
// dex:explanation text:"Record the row's current cells and complete the Flow."
type completeRowIntake struct {
	dex.StepDefaultsNoWaitFor[spreadsheet.GetValuesResult]
}

func (completeRowIntake) GetStepType() string { return completeRowIntakeStepType }

func (completeRowIntake) Execute(ctx dex.Context, result spreadsheet.GetValuesResult) (*dex.StepDecision, error) {
	outcome, err := addedRowAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	// Sheets omits values for a row that is now empty, so the read-back is then an empty cell list.
	outcome.CurrentCells = []string{}
	if len(result.Value.Values) > 0 {
		outcome.CurrentCells = result.Value.Values[0]
	}
	if err := addedRowAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
