// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// AppendRowInput contains one row of RAW cell values to append.
type AppendRowInput struct {
	// SpreadsheetID identifies the document.
	SpreadsheetID string `json:"spreadsheetId"`
	// SheetName identifies the worksheet tab.
	SheetName string `json:"sheetName"`
	// Values are the cells written from column A.
	Values []string `json:"values"`
}

// UpdateRowInput contains an absolute row replacement.
type UpdateRowInput struct {
	// SpreadsheetID identifies the document.
	SpreadsheetID string `json:"spreadsheetId"`
	// SheetName identifies the worksheet tab.
	SheetName string `json:"sheetName"`
	// RowNumber is the one-based row position.
	RowNumber int64 `json:"rowNumber"`
	// Values are the cells written from column A.
	Values []string `json:"values"`
}

// RowWriteResult locates the row accepted by Google.
type RowWriteResult struct {
	// RowNumber is the one-based row position.
	RowNumber int64 `json:"rowNumber"`
	// UpdatedRange is the A1 range written by Google.
	UpdatedRange string `json:"updatedRange"`
}

// AppendRowOperation appends a row with a bounded repeat budget.
type AppendRowOperation struct{ client *Client }

// Definition returns the generated append contract.
func (AppendRowOperation) Definition() sdkgo.MutationDefinition { return AppendRowDefinition }

// IdempotencyKey identifies the logical append; Google does not deduplicate it.
func (AppendRowOperation) IdempotencyKey(callID sdkgo.CallID, _ AppendRowInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// AppendRow binds the append Mutation to the client.
func (client *Client) AppendRow() AppendRowOperation { return AppendRowOperation{client} }

// Invoke sends one append per fenced attempt, then stops after the send cap.
func (operation AppendRowOperation) Invoke(call sdkgo.Call, input AppendRowInput) sdkgo.MutationAttempt[RowWriteResult] {
	if !validRowWriteInput(input.SpreadsheetID, input.SheetName, input.Values) {
		return spreadsheetDocumentMutationBranch(AppendRowBranchDefect, RowWriteResult{}, sdkgo.FailureValidation, "spreadsheet, tab, or values are invalid")
	}
	fence, err := singledispatch.NewFence(call, AppendRowDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return spreadsheetDocumentMutationBranch(AppendRowBranchDefect, RowWriteResult{}, sdkgo.FailureLocalDefect, "append checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return spreadsheetDocumentMutationBranch(AppendRowBranchDefect, RowWriteResult{}, sdkgo.FailureLocalDefect, "append checkpoint is invalid")
	}
	credential, failure, isTerminal := operation.client.resolveCredential(call.Context, call, "appendRow")
	if failure != nil {
		if isTerminal {
			return spreadsheetDocumentMutationBranch(AppendRowBranchDefect, RowWriteResult{}, failure.Kind, failure.Message)
		}
		return sdkgo.NewMutationRetry[RowWriteResult](*failure, 0)
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		if decision.Marker.ProviderID != "" {
			result, isConfirmed := operation.readBackAppendedRow(call, credential, input, decision.Marker.ProviderID)
			if isConfirmed {
				return sdkgo.NewMutationBranch(AppendRowBranchAppended, result, nil, spreadsheetDocumentReceipt(call, input.SpreadsheetID+"#"+result.UpdatedRange))
			}
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return spreadsheetDocumentMutationBranch(AppendRowBranchUncertain, RowWriteResult{}, sdkgo.FailureAvailability, "append may have succeeded before the send cap")
		}
	}
	targetRange := quoteSheet(input.SheetName)
	query := url.Values{"valueInputOption": {"RAW"}, "insertDataOption": {"INSERT_ROWS"}}
	payload := map[string]any{"range": targetRange, "majorDimension": "ROWS", "values": [][]string{input.Values}}
	var response requestResult
	var requestError error
	if err := fence.Dispatch(func(dispatchContext context.Context) error {
		response, _, requestError = operation.client.request(dispatchContext, call, credential, http.MethodPost, input.SpreadsheetID, "/values/"+url.PathEscape(targetRange)+":append", query, payload)
		return nil
	}); err != nil {
		return sdkgo.NewMutationRetry[RowWriteResult](sheetFailure(sdkgo.FailureAvailability, "appendRow", "append could not be dispatched safely"), 0)
	}
	if requestError != nil {
		var providerError *providerRequestError
		if errors.As(requestError, &providerError) && providerError.undispatched {
			_ = fence.RecordCanResend()
		}
		return sdkgo.NewMutationRetry[RowWriteResult](sheetFailure(sdkgo.FailureTransport, "appendRow", "append request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[RowWriteResult](sheetFailure(sdkgo.FailureAvailability, "appendRow", "append refusal checkpoint failed"), 0)
		}
		if response.status == http.StatusTooManyRequests {
			return sdkgo.NewMutationRetry[RowWriteResult](sheetFailure(sdkgo.FailureRateLimit, "appendRow", "Google rate limited the append"), 0)
		}
		return sdkgo.NewMutationRetry[RowWriteResult](sheetFailure(sdkgo.FailureAvailability, "appendRow", "Google temporarily refused the append"), 0)
	}
	if response.status >= 500 || response.status == http.StatusRequestTimeout {
		return sdkgo.NewMutationRetry[RowWriteResult](sheetFailure(sdkgo.FailureAvailability, "appendRow", "append outcome is unknown"), 0)
	}
	if response.status == http.StatusNotFound {
		return spreadsheetDocumentMutationBranch(AppendRowBranchNotFound, RowWriteResult{}, sdkgo.FailureNotFound, "spreadsheet or tab was not found")
	}
	if response.status != http.StatusOK {
		return spreadsheetDocumentMutationBranch(AppendRowBranchProviderRejected, RowWriteResult{}, sdkgo.FailureProviderRejection, "Google rejected append with HTTP "+strconv.Itoa(response.status))
	}
	var wire updateResponse
	if json.Unmarshal(response.body, &wire) != nil || wire.Updates == nil || wire.Updates.UpdatedRange == "" {
		return spreadsheetDocumentMutationBranch(AppendRowBranchInvalidResponse, RowWriteResult{}, sdkgo.FailureProtocol, "Google accepted append but returned no row locator")
	}
	rowNumber, hasRow := firstRowNumber(wire.Updates.UpdatedRange)
	if !hasRow {
		return spreadsheetDocumentMutationBranch(AppendRowBranchInvalidResponse, RowWriteResult{}, sdkgo.FailureProtocol, "Google returned an invalid appended row range")
	}
	if err := fence.RecordProviderID(wire.Updates.UpdatedRange); err != nil {
		return sdkgo.NewMutationRetry[RowWriteResult](sheetFailure(sdkgo.FailureAvailability, "appendRow", "append locator checkpoint failed"), 0)
	}
	result := RowWriteResult{RowNumber: rowNumber, UpdatedRange: wire.Updates.UpdatedRange}
	return sdkgo.NewMutationBranch(AppendRowBranchAppended, result, nil, spreadsheetDocumentReceipt(call, input.SpreadsheetID+"#"+result.UpdatedRange))
}

func (operation AppendRowOperation) readBackAppendedRow(call sdkgo.Call, credential Credentials, input AppendRowInput, updatedRange string) (RowWriteResult, bool) {
	rowNumber, hasRow := firstRowNumber(updatedRange)
	if !hasRow {
		return RowWriteResult{}, false
	}
	response, _, err := operation.client.request(call.Context, call, credential, http.MethodGet, input.SpreadsheetID, "/values/"+url.PathEscape(updatedRange), nil, nil)
	if err != nil || response.status != http.StatusOK {
		return RowWriteResult{}, false
	}
	values, decodeFailure := operation.client.decodeValues(response.body, "appendRow")
	if decodeFailure != nil || len(values.Values) != 1 || len(values.Values[0]) != len(input.Values) {
		return RowWriteResult{}, false
	}
	for column := range input.Values {
		if fmt.Sprint(values.Values[0][column]) != input.Values[column] {
			return RowWriteResult{}, false
		}
	}
	return RowWriteResult{RowNumber: rowNumber, UpdatedRange: updatedRange}, true
}

// UpdateRowOperation replaces a row at a known position.
type UpdateRowOperation struct{ client *Client }

// Definition returns the generated update contract.
func (UpdateRowOperation) Definition() sdkgo.MutationDefinition { return UpdateRowDefinition }

// IdempotencyKey identifies one logical replacement.
func (UpdateRowOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateRowInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// UpdateRow binds row replacement to the client.
func (client *Client) UpdateRow() UpdateRowOperation { return UpdateRowOperation{client} }

// Invoke writes the same absolute row on each attempt.
func (operation UpdateRowOperation) Invoke(call sdkgo.Call, input UpdateRowInput) sdkgo.MutationAttempt[RowWriteResult] {
	if !validRowWriteInput(input.SpreadsheetID, input.SheetName, input.Values) || input.RowNumber < 1 {
		return spreadsheetDocumentMutationBranch(UpdateRowBranchDefect, RowWriteResult{}, sdkgo.FailureValidation, "spreadsheet, tab, row, or values are invalid")
	}
	credential, failure, isTerminal := operation.client.resolveCredential(call.Context, call, "updateRow")
	if failure != nil {
		if isTerminal {
			return spreadsheetDocumentMutationBranch(UpdateRowBranchDefect, RowWriteResult{}, failure.Kind, failure.Message)
		}
		return sdkgo.NewMutationRetry[RowWriteResult](*failure, 0)
	}
	targetRange := fmt.Sprintf("%s!A%d", quoteSheet(input.SheetName), input.RowNumber)
	payload := map[string]any{"range": targetRange, "majorDimension": "ROWS", "values": [][]string{input.Values}}
	response, _, err := operation.client.request(call.Context, call, credential, http.MethodPut, input.SpreadsheetID, "/values/"+url.PathEscape(targetRange), url.Values{"valueInputOption": {"RAW"}}, payload)
	if retry, hasRetry := sheetMutationRetry[RowWriteResult](response, err, "updateRow"); hasRetry {
		return retry
	}
	if response.status == http.StatusNotFound {
		return spreadsheetDocumentMutationBranch(UpdateRowBranchNotFound, RowWriteResult{}, sdkgo.FailureNotFound, "spreadsheet or tab was not found")
	}
	if response.status != http.StatusOK {
		return spreadsheetDocumentMutationBranch(UpdateRowBranchProviderRejected, RowWriteResult{}, sdkgo.FailureProviderRejection, "Google rejected row update with HTTP "+strconv.Itoa(response.status))
	}
	updatedRange := fmt.Sprintf("%s!A%d:%s%d", quoteSheet(input.SheetName), input.RowNumber, columnName(len(input.Values)), input.RowNumber)
	var wire updateResponse
	if json.Unmarshal(response.body, &wire) == nil && strings.TrimSpace(wire.UpdatedRange) != "" {
		updatedRange = wire.UpdatedRange
	}
	result := RowWriteResult{RowNumber: input.RowNumber, UpdatedRange: updatedRange}
	return sdkgo.NewMutationBranch(UpdateRowBranchUpdated, result, nil, spreadsheetDocumentReceipt(call, input.SpreadsheetID+"#"+updatedRange))
}

func validRowWriteInput(spreadsheetID, sheetName string, values []string) bool {
	return validSpreadsheetID(spreadsheetID) && validSheetTitle(sheetName) && len(values) > 0 && len(values) <= 1000
}
