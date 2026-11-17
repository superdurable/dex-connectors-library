// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package upsertcontact

import (
	"fmt"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const spreadsheetOperationsFlowType = "GoogleSheetsOperations"

var spreadsheetOperationAttribute = dex.DefineAttribute[string]("google-sheets-operation")

// SpreadsheetOperationRequest chooses one write for the example worker.
type SpreadsheetOperationRequest struct {
	// Operation is createSheet, deleteSheet, clearValues, appendRow, or updateRow.
	Operation string `json:"operation"`
	// SpreadsheetID identifies the document.
	SpreadsheetID string `json:"spreadsheetId"`
	// SheetName is the tab title for row operations or creation.
	SheetName string `json:"sheetName"`
	// SheetID is the numeric tab ID for deletion.
	SheetID int64 `json:"sheetId"`
	// Range is the A1 range for clearing.
	Range string `json:"range"`
	// RowNumber is the one-based position for replacement.
	RowNumber int64 `json:"rowNumber"`
	// Values are RAW cells for row writes.
	Values []string `json:"values"`
}

// SpreadsheetOperationsFlow exercises tab, range, and row writes in Dex.
type SpreadsheetOperationsFlow struct {
	dex.FlowDefaults
	connection spreadsheet.Connection
}

// NewSpreadsheetOperationsFlow binds the connector connection at registration.
func NewSpreadsheetOperationsFlow(connection spreadsheet.Connection) *SpreadsheetOperationsFlow {
	return &SpreadsheetOperationsFlow{connection: connection}
}

func (*SpreadsheetOperationsFlow) GetFlowType() string { return spreadsheetOperationsFlowType }
func (*SpreadsheetOperationsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{spreadsheetOperationAttribute}}
}
func (flow *SpreadsheetOperationsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSpreadsheetOperation{}),
		dex.DefineStep(spreadsheet.NewCreateSheetStep(spreadsheet.CreateSheetStepConfig[spreadsheet.CreateSheetInput]{
			StepType: "CallCreateSheet", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Sheets", Explanation: "Add a named worksheet tab."},
			MapToOperationInput: mapCreateSheetInput, Created: sdkgo.GoTo(completeCreateSheet{}),
		})),
		dex.DefineStep(spreadsheet.NewDeleteSheetStep(spreadsheet.DeleteSheetStepConfig[spreadsheet.DeleteSheetInput]{
			StepType: "CallDeleteSheet", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Sheets", Explanation: "Remove a worksheet tab by ID."},
			MapToOperationInput: mapDeleteSheetInput, Deleted: sdkgo.GoTo(completeDeleteSheet{}),
		})),
		dex.DefineStep(spreadsheet.NewClearValuesStep(spreadsheet.ClearValuesStepConfig[spreadsheet.ClearValuesInput]{
			StepType: "CallClearValues", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Sheets", Explanation: "Clear values in the selected A1 range."},
			MapToOperationInput: mapClearValuesInput, Cleared: sdkgo.GoTo(completeClearValues{}),
		})),
		dex.DefineStep(spreadsheet.NewAppendRowStep(spreadsheet.AppendRowStepConfig[spreadsheet.AppendRowInput]{
			StepType: "CallAppendRow", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Sheets", Explanation: "Append one row with a bounded repeat budget."},
			MapToOperationInput: mapAppendRowInput, Appended: sdkgo.GoTo(completeAppendRow{}),
		})),
		dex.DefineStep(spreadsheet.NewUpdateRowStep(spreadsheet.UpdateRowStepConfig[spreadsheet.UpdateRowInput]{
			StepType: "CallUpdateRow", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Sheets", Explanation: "Replace a row at its known position."},
			MapToOperationInput: mapUpdateRowInput, Updated: sdkgo.GoTo(completeUpdateRow{}),
		})),
		dex.DefineStep(completeCreateSheet{}),
		dex.DefineStep(completeDeleteSheet{}),
		dex.DefineStep(completeClearValues{}),
		dex.DefineStep(completeAppendRow{}),
		dex.DefineStep(completeUpdateRow{}),
	}
}
func (flow *SpreadsheetOperationsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-sheets-operation value-type:string editable:false description:"Selected spreadsheet operation"
// GetDexSummary shows the requested operation in Dex Web.
func (*SpreadsheetOperationsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	operation, err := spreadsheetOperationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-sheets-operation": operation}}, nil
}

// dex:field attribute-key:google-sheets-operation value-type:string editable:false description:"Selected spreadsheet operation"
// GetDexDisplay shows the requested operation in Dex Web.
func (*SpreadsheetOperationsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	operation, err := spreadsheetOperationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-sheets-operation": operation}}, nil
}

func mapCreateSheetInput(input spreadsheet.CreateSheetInput) spreadsheet.CreateSheetInput {
	return input
}
func mapDeleteSheetInput(input spreadsheet.DeleteSheetInput) spreadsheet.DeleteSheetInput {
	return input
}
func mapClearValuesInput(input spreadsheet.ClearValuesInput) spreadsheet.ClearValuesInput {
	return input
}
func mapAppendRowInput(input spreadsheet.AppendRowInput) spreadsheet.AppendRowInput { return input }
func mapUpdateRowInput(input spreadsheet.UpdateRowInput) spreadsheet.UpdateRowInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Route the selected sheet or row write to its provider Step."
type startSpreadsheetOperation struct{ dex.StepDefaults }

func (startSpreadsheetOperation) GetStepType() string { return "StartSpreadsheetOperation" }
func (startSpreadsheetOperation) WaitFor(dex.Context, SpreadsheetOperationRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSpreadsheetOperation) Execute(ctx dex.Context, input SpreadsheetOperationRequest) (*dex.StepDecision, error) {
	if err := spreadsheetOperationAttribute.Set(ctx, input.Operation); err != nil {
		return nil, err
	}
	switch input.Operation {
	case "createSheet":
		return dex.GoTo(sdkgo.StepRef[spreadsheet.CreateSheetInput]("CallCreateSheet"), spreadsheet.CreateSheetInput{SpreadsheetID: input.SpreadsheetID, Title: input.SheetName}), nil
	case "deleteSheet":
		return dex.GoTo(sdkgo.StepRef[spreadsheet.DeleteSheetInput]("CallDeleteSheet"), spreadsheet.DeleteSheetInput{SpreadsheetID: input.SpreadsheetID, SheetID: input.SheetID}), nil
	case "clearValues":
		return dex.GoTo(sdkgo.StepRef[spreadsheet.ClearValuesInput]("CallClearValues"), spreadsheet.ClearValuesInput{SpreadsheetID: input.SpreadsheetID, Range: input.Range}), nil
	case "appendRow":
		return dex.GoTo(sdkgo.StepRef[spreadsheet.AppendRowInput]("CallAppendRow"), spreadsheet.AppendRowInput{SpreadsheetID: input.SpreadsheetID, SheetName: input.SheetName, Values: input.Values}), nil
	case "updateRow":
		return dex.GoTo(sdkgo.StepRef[spreadsheet.UpdateRowInput]("CallUpdateRow"), spreadsheet.UpdateRowInput{SpreadsheetID: input.SpreadsheetID, SheetName: input.SheetName, RowNumber: input.RowNumber, Values: input.Values}), nil
	default:
		return nil, fmt.Errorf("unknown spreadsheet operation %q", input.Operation)
	}
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the created tab."
type completeCreateSheet struct {
	dex.StepDefaultsNoWaitFor[spreadsheet.CreateSheetResult]
}

func (completeCreateSheet) GetStepType() string { return "CompleteCreateSheet" }
func (completeCreateSheet) Execute(_ dex.Context, result spreadsheet.CreateSheetResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the deleted tab identity."
type completeDeleteSheet struct {
	dex.StepDefaultsNoWaitFor[spreadsheet.DeleteSheetResult]
}

func (completeDeleteSheet) GetStepType() string { return "CompleteDeleteSheet" }
func (completeDeleteSheet) Execute(_ dex.Context, result spreadsheet.DeleteSheetResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the cleared range."
type completeClearValues struct {
	dex.StepDefaultsNoWaitFor[spreadsheet.ClearValuesResult]
}

func (completeClearValues) GetStepType() string { return "CompleteClearValues" }
func (completeClearValues) Execute(_ dex.Context, result spreadsheet.ClearValuesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the appended row location."
type completeAppendRow struct {
	dex.StepDefaultsNoWaitFor[spreadsheet.AppendRowResult]
}

func (completeAppendRow) GetStepType() string { return "CompleteAppendRow" }
func (completeAppendRow) Execute(_ dex.Context, result spreadsheet.AppendRowResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the replaced row location."
type completeUpdateRow struct {
	dex.StepDefaultsNoWaitFor[spreadsheet.UpdateRowResult]
}

func (completeUpdateRow) GetStepType() string { return "CompleteUpdateRow" }
func (completeUpdateRow) Execute(_ dex.Context, result spreadsheet.UpdateRowResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SpreadsheetOperationsFlow)(nil)
