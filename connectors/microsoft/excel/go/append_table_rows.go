// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	appendTableRowsOperationID = "appendTableRows"
	// unknownAppendOutcomeRetryDelay gives a queued append time to land before the next attempt checks the key column.
	unknownAppendOutcomeRetryDelay = 10 * time.Second
)

// AppendTableRowsInput is a batch of rows for one Excel table.
type AppendTableRowsInput struct {
	// DriveID is the Microsoft Graph drive ID of the OneDrive or SharePoint
	// document library that holds the workbook, as the workbookPicker unit stores it.
	DriveID string `json:"driveId"`
	// WorkbookID is the workbook's drive item ID, as the workbookPicker unit stores it.
	WorkbookID string `json:"workbookId"`
	// Table is the table's ID, as the tablePicker unit stores it, or its name,
	// such as Decisions. An ID keeps working after the table is renamed.
	Table string `json:"table"`
	// KeyColumn is the exact name of the table column whose value identifies a
	// row, such as RequestId. A row whose key the column already holds is not
	// appended again, whichever Flow or person added it.
	KeyColumn string `json:"keyColumn"`
	// Rows lists the rows to append, in order, each mapping column names to
	// values. Every row sets KeyColumn to non-empty text, a number, or a Boolean
	// that no other row uses; a column a row leaves out is written empty. The
	// rows times the table's columns may cover at most the configured maxCells.
	Rows []map[string]CellValue `json:"rows"`
}

// AppendTableRowsOutput reports which requested rows the table now holds and how.
type AppendTableRowsOutput struct {
	// AppendedKeys lists, in input order, the keys of the rows this attempt appended.
	AppendedKeys []CellValue `json:"appendedKeys"`
	// AlreadyPresentKeys lists, in input order, the keys the key column already
	// held, whose rows were therefore not appended again.
	AlreadyPresentKeys []CellValue `json:"alreadyPresentKeys"`
	// IsFromEarlierAttempt reports that an earlier attempt of the same Step
	// execution sent the append and this attempt found every key in the table.
	IsFromEarlierAttempt bool `json:"isFromEarlierAttempt,omitempty"`
}

// AppendTableRowsOperation implements the appendTableRows connector operation.
type AppendTableRowsOperation struct{ client *Client }

type appendTableRowsRequestBody struct {
	Values [][]any `json:"values"`
}

var appendTableRowsReadBranches = readBranches{
	operationID: appendTableRowsOperationID, notFound: AppendTableRowsBranchNotFound, tooLarge: AppendTableRowsBranchInvalidResponse,
	providerRejected: AppendTableRowsBranchProviderRejected, invalidResponse: AppendTableRowsBranchInvalidResponse,
	defect: AppendTableRowsBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (AppendTableRowsOperation) Definition() sdkgo.MutationDefinition {
	return AppendTableRowsDefinition
}

// IdempotencyKey returns the Call ID. Excel has no idempotency key; the key
// column and the Step's single-dispatch checkpoint prevent duplicate rows.
func (AppendTableRowsOperation) IdempotencyKey(callID sdkgo.CallID, _ AppendTableRowsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the table's columns and its key column, then appends only the
// rows whose keys are missing, in one request. Before sending, it records a
// Dex heartbeat checkpoint. A later attempt of the same Step execution that
// finds the checkpoint never sends again: it selects appended when the key
// column holds every key, and uncertain otherwise. An append that Microsoft
// provably refused clears the checkpoint, and one whose outcome is unknown is
// retried so the next attempt can find its rows in the key column.
func (operation AppendTableRowsOperation) Invoke(call sdkgo.Call, input AppendTableRowsInput) sdkgo.MutationAttempt[AppendTableRowsOutput] {
	client := operation.client
	keys, err := validateAppendTableRowsInput(input, client.maxCells)
	if err != nil {
		return sdkgo.NewMutationBranch(AppendTableRowsBranchDefect, AppendTableRowsOutput{}, excelFailurePointer(sdkgo.FailureValidation, appendTableRowsOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, appendTableRowsOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(AppendTableRowsBranchDefect, AppendTableRowsOutput{}, failure, sdkgo.Receipt{})
	}
	columns, _, outcome := client.readTableColumns(call, &credential, appendTableRowsReadBranches, input.DriveID, input.WorkbookID, input.Table)
	if outcome != nil {
		return mutationAttemptFromReadOutcome[AppendTableRowsOutput](outcome)
	}
	keyColumn, isKnown := findTableColumn(columns, input.KeyColumn)
	if !isKnown || !rowsUseOnlyTableColumns(input.Rows, columns) {
		return sdkgo.NewMutationBranch(AppendTableRowsBranchColumnsMismatch, AppendTableRowsOutput{}, excelFailurePointer(sdkgo.FailureValidation, appendTableRowsOperationID, "the table has no column with the key column's name or with a name a row uses"), sdkgo.Receipt{})
	}
	if int64(len(input.Rows))*int64(len(columns)) > client.maxCells {
		return sdkgo.NewMutationBranch(AppendTableRowsBranchDefect, AppendTableRowsOutput{}, excelFailurePointer(sdkgo.FailureValidation, appendTableRowsOperationID, "rows times table columns exceed the configured cell limit"), sdkgo.Receipt{})
	}
	storedKeys, lookupReceipt, outcome := client.readKeyColumn(call, &credential, input, keyColumn)
	if outcome != nil {
		return mutationAttemptFromReadOutcome[AppendTableRowsOutput](outcome)
	}
	presence := splitKeysByPresence(keys, storedKeys)
	if len(presence.missingRowIndexes) == 0 {
		return sdkgo.NewMutationBranch(AppendTableRowsBranchAppended, AppendTableRowsOutput{
			AppendedKeys: []CellValue{}, AlreadyPresentKeys: presence.presentKeys, IsFromEarlierAttempt: hasEarlierDispatch(call),
		}, nil, lookupReceipt)
	}
	body, err := encodeAppendedRows(input.Rows, presence.missingRowIndexes, columns)
	if err != nil {
		return sdkgo.NewMutationBranch(AppendTableRowsBranchDefect, AppendTableRowsOutput{}, excelFailurePointer(sdkgo.FailureLocalDefect, appendTableRowsOperationID, "rows could not be encoded"), sdkgo.Receipt{})
	}
	switch claimSingleDispatch(call) {
	case singleDispatchNotRecorded:
		return sdkgo.NewMutationRetry[AppendTableRowsOutput](excelFailure(sdkgo.FailureAvailability, appendTableRowsOperationID, "Dex did not record the dispatch checkpoint; nothing was sent"), 0)
	case singleDispatchAlreadySent:
		return sdkgo.NewMutationUncertain(AppendTableRowsOutput{AppendedKeys: []CellValue{}, AlreadyPresentKeys: presence.presentKeys},
			excelFailure(sdkgo.FailureTransport, appendTableRowsOperationID, "an earlier attempt of this Step sent the append, and the key column does not show every row yet"), lookupReceipt)
	}
	response, sendErr := client.sendRequest(call, &credential, graphRequest{
		method: http.MethodPost, target: tableURL(input.DriveID, input.WorkbookID, input.Table) + "/rows/add",
		body: body, responseLimit: client.maxResponseBytes,
	})
	return client.appendAttemptFromResponse(call, response, sendErr, presence)
}

// keyPresence splits the requested keys into those the table holds and the rows still to append.
type keyPresence struct {
	presentKeys       []CellValue
	missingKeys       []CellValue
	missingRowIndexes []int
}

// readKeyColumn reads the key column's data cells.
func (client *Client) readKeyColumn(
	call sdkgo.Call,
	credential *Credentials,
	input AppendTableRowsInput,
	keyColumn tableColumn,
) ([]CellValue, sdkgo.Receipt, *readOutcome) {
	response, outcome := client.sendRead(call, credential, appendTableRowsReadBranches, graphRequest{
		method: http.MethodGet, target: columnDataBodyRangeURL(input.DriveID, input.WorkbookID, input.Table, keyColumn.id),
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return nil, sdkgo.Receipt{}, outcome
	}
	receipt := client.receipt(call, response.requestID)
	body, err := decodeRange(response.body, false)
	if err != nil || body.columnCount != 1 {
		return nil, receipt, &readOutcome{
			branch: AppendTableRowsBranchInvalidResponse, receipt: receipt,
			failure: excelFailure(sdkgo.FailureProtocol, appendTableRowsOperationID, "provider returned an invalid key column response before the append was sent"),
		}
	}
	storedKeys := make([]CellValue, 0, len(body.values))
	for _, row := range body.values {
		storedKeys = append(storedKeys, row[0])
	}
	return storedKeys, receipt, nil
}

// appendAttemptFromResponse releases the checkpoint only for a response that proves Excel appended nothing.
func (client *Client) appendAttemptFromResponse(
	call sdkgo.Call,
	response graphResponse,
	sendErr error,
	presence keyPresence,
) sdkgo.MutationAttempt[AppendTableRowsOutput] {
	receipt := client.receipt(call, response.requestID)
	var transportErr *graphTransportError
	if errors.As(sendErr, &transportErr) && !transportErr.isRequestBuilt {
		releaseSingleDispatch(call)
		return sdkgo.NewMutationBranch(AppendTableRowsBranchDefect, AppendTableRowsOutput{}, excelFailurePointer(sdkgo.FailureLocalDefect, appendTableRowsOperationID, sendErr.Error()), receipt)
	}
	if sendErr != nil {
		return sdkgo.NewMutationRetry[AppendTableRowsOutput](excelFailure(sdkgo.FailureTransport, appendTableRowsOperationID,
			"append outcome is unknown; the next attempt checks the key column instead of sending again"), unknownAppendOutcomeRetryDelay)
	}
	if isSuccessStatus(response.status) {
		return sdkgo.NewMutationBranch(AppendTableRowsBranchAppended, AppendTableRowsOutput{
			AppendedKeys: presence.missingKeys, AlreadyPresentKeys: presence.presentKeys,
		}, nil, receipt)
	}
	tokens := providerhttp.ReadErrorTokens(response.body, graphErrorTokenPointers)
	kind := statusFailureKind(response.status, tokens)
	if !isAppendProvablyRefused(response.status) {
		return sdkgo.NewMutationRetry[AppendTableRowsOutput](excelFailure(kind, appendTableRowsOperationID,
			"append outcome is unknown; the next attempt checks the key column instead of sending again"), unknownAppendOutcomeRetryDelay)
	}
	releaseSingleDispatch(call)
	switch classifyGraphError(response.status, tokens) {
	case graphErrorRetryable:
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return sdkgo.NewMutationRetry[AppendTableRowsOutput](excelFailure(kind, appendTableRowsOperationID, "provider temporarily refused the append; nothing was written"), delay)
	case graphErrorNotFound:
		return sdkgo.NewMutationBranch(AppendTableRowsBranchNotFound, AppendTableRowsOutput{}, excelFailurePointer(sdkgo.FailureNotFound, appendTableRowsOperationID, "workbook or table was not found or is not visible to the connection"), receipt)
	default:
		return sdkgo.NewMutationBranch(AppendTableRowsBranchProviderRejected, AppendTableRowsOutput{}, excelFailurePointer(kind, appendTableRowsOperationID, "provider rejected the append; nothing was written"), receipt)
	}
}

// isAppendProvablyRefused treats every 4xx except 408, plus 501 and 503, as a refusal before Excel changed the table.
func isAppendProvablyRefused(status int) bool {
	switch {
	case status == http.StatusRequestTimeout:
		return false
	case status >= 400 && status < 500:
		return true
	default:
		return status == http.StatusNotImplemented || status == http.StatusServiceUnavailable
	}
}

// validateAppendTableRowsInput returns each row's key in input order.
func validateAppendTableRowsInput(input AppendTableRowsInput, maxCells int64) ([]CellValue, error) {
	if err := validateTableLocation(input.DriveID, input.WorkbookID, input.Table); err != nil {
		return nil, err
	}
	if err := validateColumnName(input.KeyColumn); err != nil {
		return nil, fmt.Errorf("keyColumn: %w", err)
	}
	if len(input.Rows) == 0 || int64(len(input.Rows)) > maxCells {
		return nil, errors.New("rows must hold at least one row and at most the configured cell limit")
	}
	keys := make([]CellValue, len(input.Rows))
	seenKeys := map[any]bool{}
	for rowIndex, row := range input.Rows {
		key, hasKey := row[input.KeyColumn]
		if !hasKey || !key.isWritableKey() {
			return nil, fmt.Errorf("row %d must set the key column to non-empty text, a finite number, or a Boolean", rowIndex+1)
		}
		if seenKeys[key.keyIdentity()] {
			return nil, fmt.Errorf("row %d repeats the key of an earlier row", rowIndex+1)
		}
		seenKeys[key.keyIdentity()] = true
		keys[rowIndex] = key
		for columnName, value := range row {
			if err := validateColumnName(columnName); err != nil {
				return nil, fmt.Errorf("row %d: %w", rowIndex+1, err)
			}
			if _, err := value.wireValue(); err != nil {
				return nil, fmt.Errorf("row %d: %w", rowIndex+1, err)
			}
		}
	}
	return keys, nil
}

func findTableColumn(columns []tableColumn, name string) (tableColumn, bool) {
	for _, column := range columns {
		if column.name == name {
			return column, true
		}
	}
	return tableColumn{}, false
}

func rowsUseOnlyTableColumns(rows []map[string]CellValue, columns []tableColumn) bool {
	for _, row := range rows {
		for columnName := range row {
			if _, isKnown := findTableColumn(columns, columnName); !isKnown {
				return false
			}
		}
	}
	return true
}

func splitKeysByPresence(keys []CellValue, storedKeys []CellValue) keyPresence {
	presence := keyPresence{presentKeys: []CellValue{}, missingKeys: []CellValue{}}
	for rowIndex, key := range keys {
		if isKeyStored(key, storedKeys) {
			presence.presentKeys = append(presence.presentKeys, key)
			continue
		}
		presence.missingKeys = append(presence.missingKeys, key)
		presence.missingRowIndexes = append(presence.missingRowIndexes, rowIndex)
	}
	return presence
}

func isKeyStored(key CellValue, storedKeys []CellValue) bool {
	for _, stored := range storedKeys {
		if key.matchesStoredKey(stored) {
			return true
		}
	}
	return false
}

// encodeAppendedRows orders each missing row's values by table column, writing absent columns empty.
func encodeAppendedRows(rows []map[string]CellValue, rowIndexes []int, columns []tableColumn) ([]byte, error) {
	values := make([][]any, 0, len(rowIndexes))
	for _, rowIndex := range rowIndexes {
		cells := make([]any, len(columns))
		for position, column := range columns {
			value, isSet := rows[rowIndex][column.name]
			if !isSet {
				value = EmptyCellValue()
			}
			wireValue, err := value.wireValue()
			if err != nil {
				return nil, err
			}
			cells[position] = wireValue
		}
		values = append(values, cells)
	}
	return json.Marshal(appendTableRowsRequestBody{Values: values})
}
