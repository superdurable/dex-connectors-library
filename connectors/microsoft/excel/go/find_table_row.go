// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const findTableRowOperationID = "findTableRow"

// FindTableRowInput locates a row by exact typed cell value.
type FindTableRowInput struct {
	// DriveID is the Graph drive that holds the workbook.
	DriveID string `json:"driveId"`
	// WorkbookID is the workbook's drive item ID.
	WorkbookID string `json:"workbookId"`
	// Table is the table ID or name.
	Table string `json:"table"`
	// Column is an exact table column name.
	Column string `json:"column"`
	// Value is the exact text, number, or Boolean to match.
	Value CellValue `json:"value"`
}

// FindTableRowOutput holds the first matching row.
type FindTableRowOutput struct {
	// Row is the matching data row.
	Row TableRow `json:"row"`
}

// FindTableRowOperation implements the findTableRow query.
type FindTableRowOperation struct{ client *Client }

var findTableRowReadBranches = readBranches{
	operationID:      findTableRowOperationID,
	notFound:         FindTableRowBranchNotFound,
	tooLarge:         FindTableRowBranchTooLarge,
	providerRejected: FindTableRowBranchProviderRejected,
	invalidResponse:  FindTableRowBranchInvalidResponse,
	defect:           FindTableRowBranchDefect,
}

// Definition returns the query definition.
func (FindTableRowOperation) Definition() sdkgo.QueryDefinition { return FindTableRowDefinition }

// Invoke reads a bounded table and selects its first exact match.
func (operation FindTableRowOperation) Invoke(call sdkgo.Call, input FindTableRowInput) sdkgo.QueryAttempt[FindTableRowOutput] {
	client := operation.client
	if err := validateTableLocation(input.DriveID, input.WorkbookID, input.Table); err != nil || input.Column == "" || input.Value.IsAbsent() {
		return sdkgo.NewQueryBranch(FindTableRowBranchDefect, FindTableRowOutput{}, excelFailurePointer(sdkgo.FailureValidation, findTableRowOperationID, "table, column, and value are required"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, findTableRowOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(FindTableRowBranchDefect, FindTableRowOutput{}, failure, sdkgo.Receipt{})
	}
	columns, _, outcome := client.readTableColumns(call, &credential, findTableRowReadBranches, input.DriveID, input.WorkbookID, input.Table)
	if outcome != nil {
		return queryAttemptFromReadOutcome[FindTableRowOutput](outcome)
	}
	columnIndex := -1
	for index, column := range columns {
		if column.name == input.Column {
			columnIndex = index
			break
		}
	}
	if columnIndex < 0 {
		return sdkgo.NewQueryBranch(FindTableRowBranchDefect, FindTableRowOutput{}, excelFailurePointer(sdkgo.FailureValidation, findTableRowOperationID, "table has no column with that name"), sdkgo.Receipt{})
	}
	response, outcome := client.sendRead(call, &credential, findTableRowReadBranches, graphRequest{method: http.MethodGet, target: tableDataBodyRangeURL(input.DriveID, input.WorkbookID, input.Table), responseLimit: client.maxResponseBytes})
	if outcome != nil {
		return queryAttemptFromReadOutcome[FindTableRowOutput](outcome)
	}
	receipt := client.receipt(call, response.requestID)
	body, err := decodeRange(response.body, false)
	switch {
	case err == nil && body.cellCount() > client.maxCells, errors.Is(err, errRangeValuesOmitted):
		return sdkgo.NewQueryBranch(FindTableRowBranchTooLarge, FindTableRowOutput{}, excelFailurePointer(sdkgo.FailureResponseTooLarge, findTableRowOperationID, "table exceeds the configured cell limit"), receipt)
	case err != nil, body.columnCount != int64(len(columns)):
		return sdkgo.NewQueryBranch(FindTableRowBranchInvalidResponse, FindTableRowOutput{}, excelFailurePointer(sdkgo.FailureProtocol, findTableRowOperationID, "provider returned invalid table rows"), receipt)
	}
	for rowIndex, cells := range body.values {
		if cells[columnIndex].value == input.Value.value {
			row := TableRow{Index: rowIndex, Values: make(map[string]CellValue, len(columns))}
			for index, column := range columns {
				row.Values[column.name] = cells[index]
			}
			return sdkgo.NewQueryBranch(FindTableRowBranchFound, FindTableRowOutput{Row: row}, nil, receipt)
		}
	}
	return sdkgo.NewQueryBranch(FindTableRowBranchNotFound, FindTableRowOutput{}, excelFailurePointer(sdkgo.FailureNotFound, findTableRowOperationID, "table has no row with that column value"), receipt)
}
