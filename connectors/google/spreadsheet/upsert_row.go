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
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

type upsertWriteAttempt struct {
	client     *Client
	call       sdkgo.Call
	credential Credentials
	input      UpsertRowInput
	query      url.Values
	pathSuffix string
	payload    any
	result     requestResult
}

// Definition returns the immutable connector operation definition.
func (UpsertRowOperation) Definition() sdkgo.MutationDefinition { return UpsertRowDefinition }

// IdempotencyKey derives a stable correlation key from the connector call ID.
func (UpsertRowOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertRowInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the key, replaces an existing row, or fences a non-idempotent append.
func (operation UpsertRowOperation) Invoke(call sdkgo.Call, input UpsertRowInput) sdkgo.MutationAttempt[UpsertRowOutput] {
	findInput := FindRowInput{SpreadsheetID: input.SpreadsheetID, SheetName: input.SheetName, KeyColumn: input.KeyColumn, KeyValue: input.KeyValue}
	if err := validateFindInput(findInput); err != nil || len(input.Values) == 0 {
		message := "values are required"
		if err != nil {
			message = err.Error()
		}
		return sdkgo.NewMutationBranch(UpsertRowBranchDefect, UpsertRowOutput{}, failurePointer(sdkgo.FailureValidation, "upsertRow", message), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, UpsertRowDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertRowBranchDefect, UpsertRowOutput{}, failurePointer(sdkgo.FailureLocalDefect, "upsertRow", "single-dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return uncertainUpsert(call, operation.client, requestResult{}, "prior dispatch checkpoint cannot be trusted")
	}
	credential, credentialFailure := operation.client.resolveCredential(call, "upsertRow")
	if credentialFailure != nil {
		return retryBeforeUpsertWrite(fence, decision.Kind, *credentialFailure, 0)
	}
	lookup, err := operation.client.getValues(call, credential, input.SpreadsheetID, quoteSheet(input.SheetName))
	if err != nil {
		if requestFailure, ok := err.(*providerRequestError); ok {
			switch requestFailure.kind {
			case sdkgo.FailureResponseTooLarge:
				if decision.Kind == singledispatch.ReadBackRequired {
					return uncertainUpsert(call, operation.client, lookup, "read-back response is oversized")
				}
				return sdkgo.NewMutationBranch(UpsertRowBranchInvalidResponse, UpsertRowOutput{}, failurePointer(requestFailure.kind, "upsertRow", requestFailure.message), operation.client.receipt(call, lookup, ""))
			case sdkgo.FailureLocalDefect:
				return sdkgo.NewMutationBranch(UpsertRowBranchDefect, UpsertRowOutput{}, failurePointer(requestFailure.kind, "upsertRow", requestFailure.message), sdkgo.Receipt{})
			}
		}
		return retryBeforeUpsertWrite(fence, decision.Kind, sheetFailure(sdkgo.FailureAvailability, "upsertRow", "sheet lookup is unavailable"), 0)
	}
	lookupReceipt := operation.client.receipt(call, lookup, "")
	retry, delay, retryAfterErr := classifyRetryableStatus(lookup.status, lookup.header)
	if retryAfterErr != nil {
		return retryBeforeUpsertWrite(fence, decision.Kind, sheetFailure(sdkgo.FailureProtocol, "upsertRow", "provider returned an invalid Retry-After header"), 0)
	}
	if retry {
		return retryBeforeUpsertWrite(fence, decision.Kind, sheetFailure(statusFailureKind(lookup.status), "upsertRow", "sheet lookup is temporarily unavailable"), delay)
	}
	if lookup.status < 200 || lookup.status >= 300 {
		if decision.Kind == singledispatch.ReadBackRequired {
			return uncertainUpsert(call, operation.client, lookup, "read-back was rejected")
		}
		return sdkgo.NewMutationBranch(UpsertRowBranchProviderRejected, UpsertRowOutput{}, failurePointer(statusFailureKind(lookup.status), "upsertRow", "provider rejected the pre-write query"), lookupReceipt)
	}
	values, decodeFailure := operation.client.decodeValues(lookup.body, "upsertRow")
	if decodeFailure != nil {
		if decision.Kind == singledispatch.ReadBackRequired {
			return uncertainUpsert(call, operation.client, lookup, "read-back response is invalid")
		}
		return sdkgo.NewMutationBranch(UpsertRowBranchInvalidResponse, UpsertRowOutput{}, decodeFailure, lookupReceipt)
	}
	rows := convertValues(values).Values
	match, branch, findFailure := findRow(rows, input.KeyColumn, input.KeyValue)
	if branch == FindRowBranchConflict {
		return sdkgo.NewMutationBranch(UpsertRowBranchConflict, UpsertRowOutput{}, findFailure, lookupReceipt)
	}
	if branch != FindRowBranchFound && branch != FindRowBranchNotFound {
		return sdkgo.NewMutationBranch(UpsertRowBranchDefect, UpsertRowOutput{}, findFailure, lookupReceipt)
	}
	row, buildFailure := buildRow(rows, input)
	if buildFailure != nil {
		return sdkgo.NewMutationBranch(UpsertRowBranchDefect, UpsertRowOutput{}, buildFailure, lookupReceipt)
	}
	if branch == FindRowBranchFound && rowMatchesInput(match, input) {
		action := "updated"
		if decision.Kind == singledispatch.ReadBackRequired && decision.Marker.State == singledispatch.StateClaimed {
			action = "inserted"
		}
		return sdkgo.NewMutationBranch(UpsertRowBranchUpserted, upsertOutput(action, match.RowNumber, input.SheetName, len(row)), nil, lookupReceipt)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		if decision.Marker.State == singledispatch.StateClaimed || branch == FindRowBranchNotFound {
			return uncertainUpsert(call, operation.client, lookup, "prior append cannot be confirmed by the row key")
		}
		// No marker can follow a safe PUT; replacing the still-present row is repeatable.
	}
	if branch == FindRowBranchFound {
		return operation.replaceRow(call, credential, input, row, match.RowNumber)
	}
	return operation.appendRow(call, credential, input, row, fence)
}

func (operation UpsertRowOperation) replaceRow(call sdkgo.Call, credential Credentials, input UpsertRowInput, row []string, rowNumber int64) sdkgo.MutationAttempt[UpsertRowOutput] {
	targetRange := fmt.Sprintf("%s!A%d", quoteSheet(input.SheetName), rowNumber)
	payload := map[string]any{"range": targetRange, "majorDimension": "ROWS", "values": [][]string{row}}
	executeTimeout := UpsertRowDefinition.StepDefaults.ExecuteMethodTimeout
	requestDeadline := time.Now().Add(executeTimeout - executeTimeout/6)
	if deadline, hasDeadline := call.Context.Deadline(); hasDeadline {
		attemptDeadline := deadline.Add(-executeTimeout / 10)
		if attemptDeadline.Before(requestDeadline) {
			requestDeadline = attemptDeadline
		}
	}
	if time.Until(requestDeadline) < executeTimeout/10 {
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(sdkgo.FailureAvailability, "upsertRow", "insufficient attempt time for row replacement"), 0)
	}
	requestContext, cancel := context.WithDeadline(call.Context, requestDeadline)
	defer cancel()
	result, err := operation.client.writeRequest(requestContext, call, credential, http.MethodPut, input.SpreadsheetID, "/values/"+url.PathEscape(targetRange), url.Values{"valueInputOption": {"RAW"}}, payload)
	if result.status == 0 {
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(sdkgo.FailureTransport, "upsertRow", "row replacement response is unavailable"), 0)
	}
	if result.status == http.StatusTooManyRequests || result.status == http.StatusConflict || result.status >= 500 {
		delay, retryAfterErr := retryAfterDelay(result.header)
		if retryAfterErr != nil {
			return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(sdkgo.FailureProtocol, "upsertRow", "provider returned an invalid Retry-After header"), 0)
		}
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(statusFailureKind(result.status), "upsertRow", "row replacement is temporarily unavailable"), delay)
	}
	receipt := operation.client.receipt(call, result, fmt.Sprintf("%s#%s!%d", input.SpreadsheetID, input.SheetName, rowNumber))
	if result.status < 200 || result.status >= 300 {
		return sdkgo.NewMutationBranch(UpsertRowBranchProviderRejected, UpsertRowOutput{}, failurePointer(statusFailureKind(result.status), "upsertRow", "provider rejected the row replacement"), receipt)
	}
	// A 2xx proves the absolute replacement was accepted even when its body is unusable.
	updatedRange := upsertOutput("updated", rowNumber, input.SheetName, len(row)).UpdatedRange
	if err == nil {
		var response updateResponse
		if json.Unmarshal(result.body, &response) == nil && response.UpdatedRange != "" {
			updatedRange = response.UpdatedRange
		}
	}
	return sdkgo.NewMutationBranch(UpsertRowBranchUpserted, UpsertRowOutput{Action: "updated", RowNumber: rowNumber, UpdatedRange: updatedRange}, nil, receipt)
}

func (operation UpsertRowOperation) appendRow(call sdkgo.Call, credential Credentials, input UpsertRowInput, row []string, fence *singledispatch.Fence) sdkgo.MutationAttempt[UpsertRowOutput] {
	targetRange := quoteSheet(input.SheetName)
	attempt := &upsertWriteAttempt{
		client: operation.client, call: call, credential: credential, input: input,
		pathSuffix: "/values/" + url.PathEscape(targetRange) + ":append",
		query:      url.Values{"valueInputOption": {"RAW"}, "insertDataOption": {"INSERT_ROWS"}},
		payload:    map[string]any{"range": targetRange, "majorDimension": "ROWS", "values": [][]string{row}},
	}
	err := fence.Dispatch(attempt.send)
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(sdkgo.FailureAvailability, "upsertRow", "append was not sent before the attempt deadline"), 0)
	}
	if errors.Is(err, singledispatch.ErrDispatchNotAllowed) {
		return uncertainUpsert(call, operation.client, attempt.result, "single-dispatch permission is unavailable")
	}
	result := attempt.result
	if result.status == http.StatusTooManyRequests || result.status == http.StatusServiceUnavailable || result.status == http.StatusConflict {
		return retryAfterRejectedAppend(fence, result)
	}
	if result.status == 0 {
		var requestFailure *providerRequestError
		if errors.As(err, &requestFailure) && requestFailure.undispatched {
			return retryAfterRejectedAppend(fence, result)
		}
		return operation.reconcileAppend(call, credential, input, row)
	}
	receipt := operation.client.receipt(call, result, "")
	if result.status >= 500 {
		return operation.reconcileAppend(call, credential, input, row)
	}
	if result.status < 200 || result.status >= 300 {
		return sdkgo.NewMutationBranch(UpsertRowBranchProviderRejected, UpsertRowOutput{}, failurePointer(statusFailureKind(result.status), "upsertRow", "provider rejected the append"), receipt)
	}
	if err == nil {
		var response updateResponse
		if json.Unmarshal(result.body, &response) == nil && response.Updates != nil && response.Updates.UpdatedRange != "" {
			updatedRange := response.Updates.UpdatedRange
			if rowNumber, hasRow := firstRowNumber(updatedRange); hasRow {
				// The range is a provider locator for later read-back, not a deduplication key.
				_ = fence.RecordProviderID(updatedRange)
				return sdkgo.NewMutationBranch(UpsertRowBranchUpserted, UpsertRowOutput{Action: "inserted", RowNumber: rowNumber, UpdatedRange: updatedRange}, nil, operation.client.receipt(call, result, updatedRange))
			}
		}
	}
	return operation.reconcileAppend(call, credential, input, row)
}

func (attempt *upsertWriteAttempt) send(ctx context.Context) error {
	var err error
	attempt.result, err = attempt.client.writeRequest(ctx, attempt.call, attempt.credential, http.MethodPost, attempt.input.SpreadsheetID, attempt.pathSuffix, attempt.query, attempt.payload)
	return err
}

func (operation UpsertRowOperation) reconcileAppend(call sdkgo.Call, credential Credentials, input UpsertRowInput, row []string) sdkgo.MutationAttempt[UpsertRowOutput] {
	result, err := operation.client.getValues(call, credential, input.SpreadsheetID, quoteSheet(input.SheetName))
	if err != nil {
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(sdkgo.FailureAvailability, "upsertRow", "append read-back is unavailable"), 0)
	}
	retry, delay, retryAfterErr := classifyRetryableStatus(result.status, result.header)
	if retryAfterErr != nil {
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(sdkgo.FailureProtocol, "upsertRow", "read-back returned an invalid Retry-After header"), 0)
	}
	if retry {
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(statusFailureKind(result.status), "upsertRow", "append read-back is temporarily unavailable"), delay)
	}
	if result.status < 200 || result.status >= 300 {
		return uncertainUpsert(call, operation.client, result, "append read-back was rejected")
	}
	values, decodeFailure := operation.client.decodeValues(result.body, "upsertRow")
	if decodeFailure != nil {
		return uncertainUpsert(call, operation.client, result, "append read-back is invalid")
	}
	match, branch, findFailure := findRow(convertValues(values).Values, input.KeyColumn, input.KeyValue)
	if branch == FindRowBranchConflict {
		return sdkgo.NewMutationBranch(UpsertRowBranchConflict, UpsertRowOutput{}, findFailure, operation.client.receipt(call, result, ""))
	}
	if branch != FindRowBranchFound || !rowMatchesInput(match, input) {
		return uncertainUpsert(call, operation.client, result, "append cannot be confirmed by the row key")
	}
	return sdkgo.NewMutationBranch(UpsertRowBranchUpserted, upsertOutput("inserted", match.RowNumber, input.SheetName, len(row)), nil, operation.client.receipt(call, result, ""))
}

func retryBeforeUpsertWrite(fence *singledispatch.Fence, decision singledispatch.DecisionKind, failure sdkgo.Failure, delay time.Duration) sdkgo.MutationAttempt[UpsertRowOutput] {
	if decision == singledispatch.SendAllowed {
		if err := fence.RecordNotSent(); err != nil {
			failure = sheetFailure(sdkgo.FailureAvailability, "upsertRow", "pre-send checkpoint failed; retry remains fail-closed")
		}
	}
	return sdkgo.NewMutationRetry[UpsertRowOutput](failure, delay)
}

func retryAfterRejectedAppend(fence *singledispatch.Fence, result requestResult) sdkgo.MutationAttempt[UpsertRowOutput] {
	if err := fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(sdkgo.FailureAvailability, "upsertRow", "append retry checkpoint failed"), 0)
	}
	delay, err := retryAfterDelay(result.header)
	if err != nil {
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(sdkgo.FailureProtocol, "upsertRow", "provider returned an invalid Retry-After header"), 0)
	}
	kind := sdkgo.FailureTransport
	if result.status != 0 {
		kind = statusFailureKind(result.status)
	}
	return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(kind, "upsertRow", "append was not processed"), delay)
}

func uncertainUpsert(call sdkgo.Call, client *Client, result requestResult, message string) sdkgo.MutationAttempt[UpsertRowOutput] {
	return sdkgo.NewMutationUncertain(UpsertRowOutput{}, sheetFailure(sdkgo.FailureTransport, "upsertRow", message), client.receipt(call, result, ""))
}

func rowMatchesInput(found FindRowOutput, input UpsertRowInput) bool {
	if found.Values[input.KeyColumn] != input.KeyValue {
		return false
	}
	for column, value := range input.Values {
		if found.Values[column] != value {
			return false
		}
	}
	return true
}

func upsertOutput(action string, rowNumber int64, sheetName string, columnCount int) UpsertRowOutput {
	return UpsertRowOutput{Action: action, RowNumber: rowNumber, UpdatedRange: fmt.Sprintf("%s!A%d:%s%d", quoteSheet(sheetName), rowNumber, columnName(columnCount), rowNumber)}
}

func columnName(columnNumber int) string {
	var column string
	for columnNumber > 0 {
		columnNumber--
		column = string(rune('A'+columnNumber%26)) + column
		columnNumber /= 26
	}
	return column
}

func firstRowNumber(updatedRange string) (int64, bool) {
	_, cellRange, hasSheet := strings.Cut(updatedRange, "!")
	if !hasSheet {
		return 0, false
	}
	start := strings.IndexFunc(cellRange, func(character rune) bool { return character >= '0' && character <= '9' })
	if start < 0 {
		return 0, false
	}
	end := start
	for end < len(cellRange) && cellRange[end] >= '0' && cellRange[end] <= '9' {
		end++
	}
	rowNumber, err := strconv.ParseInt(cellRange[start:end], 10, 64)
	return rowNumber, err == nil && rowNumber > 0
}
