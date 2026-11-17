// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
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
// rows whose keys are missing, in one request. A single-dispatch fence counts
// the possible sends of the Step execution. Because every attempt reads the
// key column first, an unknown outcome is retried: the next attempt selects
// appended when the key column holds every key, and otherwise resends while
// possible sends stay below Call.MaxSends. At the cap it selects uncertain.
// A definite refusal is never resent, and after an earlier unconfirmed send
// it selects uncertain instead of a rejection branch.
func (operation AppendTableRowsOperation) Invoke(call sdkgo.Call, input AppendTableRowsInput) sdkgo.MutationAttempt[AppendTableRowsOutput] {
	client := operation.client
	keys, err := validateAppendTableRowsInput(input, client.maxCells)
	if err != nil {
		return sdkgo.NewMutationBranch(AppendTableRowsBranchDefect, AppendTableRowsOutput{}, excelFailurePointer(sdkgo.FailureValidation, appendTableRowsOperationID, err.Error()), sdkgo.Receipt{})
	}
	attempt, attemptFailure := newAppendAttempt(call)
	if attemptFailure != nil {
		return *attemptFailure
	}
	credential, failure := client.resolveCredential(call, appendTableRowsOperationID)
	if failure != nil {
		return attempt.localFailure(AppendTableRowsBranchDefect, *failure, sdkgo.Receipt{})
	}
	columns, _, outcome := client.readTableColumns(call, &credential, appendTableRowsReadBranches, input.DriveID, input.WorkbookID, input.Table)
	if outcome != nil {
		return attempt.readFailure(outcome)
	}
	keyColumn, isKnown := findTableColumn(columns, input.KeyColumn)
	if !isKnown || !rowsUseOnlyTableColumns(input.Rows, columns) {
		return attempt.localFailure(AppendTableRowsBranchColumnsMismatch, excelFailure(sdkgo.FailureValidation, appendTableRowsOperationID, "the table has no column with the key column's name or with a name a row uses"), sdkgo.Receipt{})
	}
	if int64(len(input.Rows))*int64(len(columns)) > client.maxCells {
		return attempt.localFailure(AppendTableRowsBranchDefect, excelFailure(sdkgo.FailureValidation, appendTableRowsOperationID, "rows times table columns exceed the configured cell limit"), sdkgo.Receipt{})
	}
	storedKeys, lookupReceipt, outcome := client.readKeyColumn(call, &credential, input, keyColumn)
	if outcome != nil {
		return attempt.readFailure(outcome)
	}
	presence := splitKeysByPresence(keys, storedKeys)
	if len(presence.missingRowIndexes) == 0 {
		return sdkgo.NewMutationBranch(AppendTableRowsBranchAppended, AppendTableRowsOutput{
			AppendedKeys: []CellValue{}, AlreadyPresentKeys: presence.presentKeys, IsFromEarlierAttempt: attempt.hasPossibleEarlierSend,
		}, nil, lookupReceipt)
	}
	body, err := encodeAppendedRows(input.Rows, presence.missingRowIndexes, columns)
	if err != nil {
		return attempt.localFailure(AppendTableRowsBranchDefect, excelFailure(sdkgo.FailureLocalDefect, appendTableRowsOperationID, "rows could not be encoded"), sdkgo.Receipt{})
	}
	if attempt.isSendForbidden {
		return sdkgo.NewMutationUncertain(AppendTableRowsOutput{AppendedKeys: []CellValue{}, AlreadyPresentKeys: presence.presentKeys},
			excelFailure(sdkgo.FailureTransport, appendTableRowsOperationID, "the append reached the send cap, and the key column does not show every row yet"), lookupReceipt)
	}
	var response graphResponse
	var sendErr error
	err = attempt.fence.Dispatch(func(context.Context) error {
		response, sendErr = client.sendRequest(call, &credential, graphRequest{
			method: http.MethodPost, target: tableURL(input.DriveID, input.WorkbookID, input.Table) + "/rows/add",
			body: body, responseLimit: client.maxResponseBytes,
		})
		return sendErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) || errors.Is(err, singledispatch.ErrDispatchNotAllowed) {
		return sdkgo.NewMutationRetry[AppendTableRowsOutput](excelFailure(sdkgo.FailureAvailability, appendTableRowsOperationID, "Dex did not record the dispatch checkpoint; nothing was sent"), 0)
	}
	return client.appendAttemptFromResponse(call, attempt, response, sendErr, presence)
}

// appendAttempt is the single-dispatch state of one appendTableRows attempt.
type appendAttempt struct {
	fence    *singledispatch.Fence
	decision singledispatch.Decision
	// hasPossibleEarlierSend means a refusal or local failure cannot prove nothing was appended.
	hasPossibleEarlierSend bool
	// isSendForbidden means the send cap is reached, or an older release's marker forbids a resend.
	isSendForbidden bool
}

// newAppendAttempt decides before any credential lookup whether this attempt may send.
func newAppendAttempt(call sdkgo.Call) (appendAttempt, *sdkgo.MutationAttempt[AppendTableRowsOutput]) {
	if hasLegacyDispatchMarker(call) {
		return appendAttempt{hasPossibleEarlierSend: true, isSendForbidden: true}, nil
	}
	fence, err := singledispatch.NewFence(call, AppendTableRowsDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		failed := sdkgo.NewMutationBranch(AppendTableRowsBranchDefect, AppendTableRowsOutput{}, excelFailurePointer(sdkgo.FailureLocalDefect, appendTableRowsOperationID, "append dispatch fence could not be constructed"), sdkgo.Receipt{})
		return appendAttempt{}, &failed
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		retry := sdkgo.NewMutationRetry[AppendTableRowsOutput](excelFailure(sdkgo.FailureAvailability, appendTableRowsOperationID, "the append dispatch checkpoint is unreadable; nothing was sent in this attempt"), 0)
		return appendAttempt{}, &retry
	}
	return appendAttempt{
		fence: fence, decision: decision,
		hasPossibleEarlierSend: decision.MaximumPriorSends > 0,
		isSendForbidden:        decision.Kind == singledispatch.ReadBackRequired,
	}, nil
}

// localFailure selects branch only when no earlier send of this Step execution may have appended rows.
func (attempt appendAttempt) localFailure(branch sdkgo.BranchID, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[AppendTableRowsOutput] {
	if attempt.hasPossibleEarlierSend {
		failure.Message += "; an earlier attempt may have appended the rows"
		return sdkgo.NewMutationUncertain(AppendTableRowsOutput{AppendedKeys: []CellValue{}, AlreadyPresentKeys: []CellValue{}}, failure, receipt)
	}
	return sdkgo.NewMutationBranch(branch, AppendTableRowsOutput{}, &failure, receipt)
}

// readFailure keeps a pre-send read Retry countable and guards a terminal read branch after a possible send.
func (attempt appendAttempt) readFailure(outcome *readOutcome) sdkgo.MutationAttempt[AppendTableRowsOutput] {
	if outcome.branch == "" {
		attempt.recordNotSent()
		return sdkgo.NewMutationRetry[AppendTableRowsOutput](outcome.failure, outcome.retryAfter)
	}
	return attempt.localFailure(outcome.branch, outcome.failure, outcome.receipt)
}

// recordNotSent keeps the send count of a first-send attempt that retries before sending.
func (attempt appendAttempt) recordNotSent() {
	if attempt.fence != nil && attempt.decision.Kind == singledispatch.SendAllowed {
		// A lost notSent only overcounts sends, so the cap still holds.
		_ = attempt.fence.RecordNotSent()
	}
}

// recordCanResend records that Microsoft did not apply this attempt's append.
func (attempt appendAttempt) recordCanResend() {
	if attempt.fence != nil {
		// A lost canResend counts this send, so the cap still holds.
		_ = attempt.fence.RecordCanResend()
	}
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

// appendAttemptFromResponse records canResend only for a response that proves Excel appended nothing.
// Every other outcome is retried, and the next attempt's key column read decides whether to resend.
func (client *Client) appendAttemptFromResponse(
	call sdkgo.Call,
	attempt appendAttempt,
	response graphResponse,
	sendErr error,
	presence keyPresence,
) sdkgo.MutationAttempt[AppendTableRowsOutput] {
	receipt := client.receipt(call, response.requestID)
	var transportErr *graphTransportError
	if errors.As(sendErr, &transportErr) && !transportErr.isRequestBuilt {
		attempt.recordCanResend()
		return attempt.localFailure(AppendTableRowsBranchDefect, excelFailure(sdkgo.FailureLocalDefect, appendTableRowsOperationID, sendErr.Error()), receipt)
	}
	if sendErr != nil {
		return sdkgo.NewMutationRetry[AppendTableRowsOutput](excelFailure(sdkgo.FailureTransport, appendTableRowsOperationID,
			"append outcome is unknown; the next attempt checks the key column before any resend"), unknownAppendOutcomeRetryDelay)
	}
	if isSuccessStatus(response.status) {
		return sdkgo.NewMutationBranch(AppendTableRowsBranchAppended, AppendTableRowsOutput{
			AppendedKeys: presence.missingKeys, AlreadyPresentKeys: presence.presentKeys,
		}, nil, receipt)
	}
	tokens := providerhttp.ReadErrorTokens(response.body, graphErrorTokenPointers)
	kind := statusFailureKind(response.status, tokens)
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		attempt.recordCanResend()
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return sdkgo.NewMutationRetry[AppendTableRowsOutput](excelFailure(kind, appendTableRowsOperationID, "provider temporarily refused the append; nothing was written"), delay)
	}
	if !isDefiniteAppendRefusal(response) {
		return sdkgo.NewMutationRetry[AppendTableRowsOutput](excelFailure(kind, appendTableRowsOperationID,
			"append outcome is unknown; the next attempt checks the key column before any resend"), unknownAppendOutcomeRetryDelay)
	}
	attempt.recordCanResend()
	errorClass := classifyGraphError(response.status, tokens)
	if errorClass == graphErrorRetryable {
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return sdkgo.NewMutationRetry[AppendTableRowsOutput](excelFailure(kind, appendTableRowsOperationID, "provider temporarily refused the append; nothing was written"), delay)
	}
	if attempt.hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(AppendTableRowsOutput{AppendedKeys: []CellValue{}, AlreadyPresentKeys: presence.presentKeys},
			excelFailure(kind, appendTableRowsOperationID, "provider refused the append after an earlier unconfirmed send, and the key column does not show every row"), receipt)
	}
	switch errorClass {
	case graphErrorNotFound:
		return sdkgo.NewMutationBranch(AppendTableRowsBranchNotFound, AppendTableRowsOutput{}, excelFailurePointer(sdkgo.FailureNotFound, appendTableRowsOperationID, "workbook or table was not found or is not visible to the connection"), receipt)
	default:
		return sdkgo.NewMutationBranch(AppendTableRowsBranchProviderRejected, AppendTableRowsOutput{}, excelFailurePointer(kind, appendTableRowsOperationID, "provider rejected the append; nothing was written"), receipt)
	}
}

// isDefiniteAppendRefusal reports a refusal status whose body carries no appended table row.
func isDefiniteAppendRefusal(response graphResponse) bool {
	if !isDefiniteRefusalStatus(response.status) {
		return false
	}
	var acceptedRow struct {
		Index *int `json:"index"`
	}
	return json.Unmarshal(response.body, &acceptedRow) != nil || acceptedRow.Index == nil
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
