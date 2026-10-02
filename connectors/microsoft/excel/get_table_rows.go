// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getTableRowsOperationID = "getTableRows"

// GetTableRowsInput identifies one Excel table.
type GetTableRowsInput struct {
	// DriveID is the Microsoft Graph drive ID of the OneDrive or SharePoint
	// document library that holds the workbook, as the workbookPicker unit stores it.
	DriveID string `json:"driveId"`
	// WorkbookID is the workbook's drive item ID, as the workbookPicker unit stores it.
	WorkbookID string `json:"workbookId"`
	// Table is the table's ID, as the tablePicker unit stores it, or its name,
	// such as ApprovalPolicy. An ID keeps working after the table is renamed.
	Table string `json:"table"`
}

// GetTableRowsOutput is every data row of one table.
type GetTableRowsOutput struct {
	// Columns lists the table's column names in table order.
	Columns []string `json:"columns"`
	// Rows lists the table's data rows in table order, without the header and
	// total rows. A table always has at least one data row, which is empty in a
	// new table.
	Rows []TableRow `json:"rows"`
}

// TableRow is one data row of an Excel table.
type TableRow struct {
	// Index is the row's zero-based position among the table's data rows.
	Index int `json:"index"`
	// Values maps every column name to the row's cell value in that column.
	Values map[string]CellValue `json:"values"`
}

// Value returns the row's cell in the named column, compared exactly, and
// whether the table has that column.
func (row TableRow) Value(column string) (CellValue, bool) {
	value, isFound := row.Values[column]
	return value, isFound
}

// GetTableRowsOperation implements the getTableRows connector operation.
type GetTableRowsOperation struct{ client *Client }

var getTableRowsReadBranches = readBranches{
	operationID: getTableRowsOperationID, notFound: GetTableRowsBranchNotFound, tooLarge: GetTableRowsBranchTooLarge,
	providerRejected: GetTableRowsBranchProviderRejected, invalidResponse: GetTableRowsBranchInvalidResponse,
	defect: GetTableRowsBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetTableRowsOperation) Definition() sdkgo.QueryDefinition { return GetTableRowsDefinition }

// Invoke reads the table's column names, then its data body range, and keys
// every row by column name. A column that changes between the two requests
// selects invalidResponse, and a table above the cell limit selects tooLarge.
func (operation GetTableRowsOperation) Invoke(call sdkgo.Call, input GetTableRowsInput) sdkgo.QueryAttempt[GetTableRowsOutput] {
	client := operation.client
	if err := validateTableLocation(input.DriveID, input.WorkbookID, input.Table); err != nil {
		return sdkgo.NewQueryBranch(GetTableRowsBranchDefect, GetTableRowsOutput{}, excelFailurePointer(sdkgo.FailureValidation, getTableRowsOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, getTableRowsOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetTableRowsBranchDefect, GetTableRowsOutput{}, failure, sdkgo.Receipt{})
	}
	columns, _, outcome := client.readTableColumns(call, &credential, getTableRowsReadBranches, input.DriveID, input.WorkbookID, input.Table)
	if outcome != nil {
		return queryAttemptFromReadOutcome[GetTableRowsOutput](outcome)
	}
	response, outcome := client.sendRead(call, &credential, getTableRowsReadBranches, graphRequest{
		method: http.MethodGet, target: tableDataBodyRangeURL(input.DriveID, input.WorkbookID, input.Table),
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return queryAttemptFromReadOutcome[GetTableRowsOutput](outcome)
	}
	receipt := client.receipt(call, response.requestID)
	body, err := decodeRange(response.body, false)
	switch {
	case err == nil && body.cellCount() > client.maxCells, errors.Is(err, errRangeValuesOmitted):
		return sdkgo.NewQueryBranch(GetTableRowsBranchTooLarge, GetTableRowsOutput{}, excelFailurePointer(sdkgo.FailureResponseTooLarge, getTableRowsOperationID, "table holds more cells than the configured cell limit"), receipt)
	case err != nil:
		return sdkgo.NewQueryBranch(GetTableRowsBranchInvalidResponse, GetTableRowsOutput{}, excelFailurePointer(sdkgo.FailureProtocol, getTableRowsOperationID, "provider returned an invalid table data response"), receipt)
	case body.columnCount != int64(len(columns)):
		return sdkgo.NewQueryBranch(GetTableRowsBranchInvalidResponse, GetTableRowsOutput{}, excelFailurePointer(sdkgo.FailureProtocol, getTableRowsOperationID, "table columns changed while the table was read; read it again"), receipt)
	}
	return sdkgo.NewQueryBranch(GetTableRowsBranchRead, keyRowsByColumnName(columns, body.values), nil, receipt)
}

func keyRowsByColumnName(columns []tableColumn, values [][]CellValue) GetTableRowsOutput {
	output := GetTableRowsOutput{Columns: make([]string, len(columns)), Rows: make([]TableRow, 0, len(values))}
	for position, column := range columns {
		output.Columns[position] = column.name
	}
	for rowIndex, cells := range values {
		row := TableRow{Index: rowIndex, Values: make(map[string]CellValue, len(columns))}
		for position, column := range columns {
			row.Values[column.name] = cells[position]
		}
		output.Rows = append(output.Rows, row)
	}
	return output
}

func validateTableLocation(driveID string, workbookID string, table string) error {
	if err := validateWorkbookLocation(driveID, workbookID); err != nil {
		return err
	}
	return validateTableReference(table)
}
