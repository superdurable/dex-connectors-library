// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import "github.com/superdurable/dex-connectors-library/sdkgo"

const listTableColumnsOperationID = "listTableColumns"

// ListTableColumnsInput identifies a table in a workbook.
type ListTableColumnsInput struct {
	// DriveID is the Graph drive that holds the workbook.
	DriveID string `json:"driveId"`
	// WorkbookID is the workbook's drive item ID.
	WorkbookID string `json:"workbookId"`
	// Table is the table ID or name.
	Table string `json:"table"`
}

// TableColumn describes one table column in table order.
type TableColumn struct {
	// ID is the Graph column ID.
	ID string `json:"id"`
	// Index is the zero-based position.
	Index int `json:"index"`
	// Name is the displayed column name.
	Name string `json:"name"`
}

// ListTableColumnsOutput holds ordered table columns.
type ListTableColumnsOutput struct {
	// Columns are ordered by Index.
	Columns []TableColumn `json:"columns"`
}

// ListTableColumnsOperation implements the listTableColumns query.
type ListTableColumnsOperation struct{ client *Client }

var listTableColumnsReadBranches = readBranches{
	operationID:      listTableColumnsOperationID,
	notFound:         ListTableColumnsBranchNotFound,
	tooLarge:         ListTableColumnsBranchTooLarge,
	providerRejected: ListTableColumnsBranchProviderRejected,
	invalidResponse:  ListTableColumnsBranchInvalidResponse,
	defect:           ListTableColumnsBranchDefect,
}

// Definition returns the query definition.
func (ListTableColumnsOperation) Definition() sdkgo.QueryDefinition {
	return ListTableColumnsDefinition
}

// Invoke reads and validates the table's ordered columns.
func (operation ListTableColumnsOperation) Invoke(call sdkgo.Call, input ListTableColumnsInput) sdkgo.QueryAttempt[ListTableColumnsOutput] {
	if err := validateTableLocation(input.DriveID, input.WorkbookID, input.Table); err != nil {
		return sdkgo.NewQueryBranch(ListTableColumnsBranchDefect, ListTableColumnsOutput{}, excelFailurePointer(sdkgo.FailureValidation, listTableColumnsOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, listTableColumnsOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListTableColumnsBranchDefect, ListTableColumnsOutput{}, failure, sdkgo.Receipt{})
	}
	columns, response, outcome := operation.client.readTableColumns(call, &credential, listTableColumnsReadBranches, input.DriveID, input.WorkbookID, input.Table)
	if outcome != nil {
		return queryAttemptFromReadOutcome[ListTableColumnsOutput](outcome)
	}
	output := ListTableColumnsOutput{Columns: make([]TableColumn, len(columns))}
	for index, column := range columns {
		output.Columns[index] = TableColumn{ID: column.id, Index: column.index, Name: column.name}
	}
	return sdkgo.NewQueryBranch(ListTableColumnsBranchListed, output, nil, operation.client.receipt(call, response.requestID))
}
