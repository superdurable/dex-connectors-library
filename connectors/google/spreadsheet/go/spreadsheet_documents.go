// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// GetSpreadsheetInput identifies a spreadsheet selected by the application.
type GetSpreadsheetInput struct {
	// SpreadsheetID is the ID from a spreadsheet URL or Google Picker.
	SpreadsheetID string `json:"spreadsheetId"`
}

// CreateSpreadsheetInput names a new spreadsheet.
type CreateSpreadsheetInput struct {
	// Title is the non-empty spreadsheet title.
	Title string `json:"title"`
}

// DeleteSpreadsheetInput identifies an app-accessible spreadsheet to remove.
type DeleteSpreadsheetInput struct {
	// SpreadsheetID is the ID from a spreadsheet URL or Google Picker.
	SpreadsheetID string `json:"spreadsheetId"`
}

// SpreadsheetTab identifies one sheet within a spreadsheet.
type SpreadsheetTab struct {
	// SheetID is Google's stable numeric tab identity.
	SheetID int64 `json:"sheetId"`
	// Title is the tab's current title.
	Title string `json:"title"`
}

// SpreadsheetInfo contains the spreadsheet title and tab identities.
type SpreadsheetInfo struct {
	// SpreadsheetID is Google's document identity.
	SpreadsheetID string `json:"spreadsheetId"`
	// Title is the spreadsheet title.
	Title string `json:"title"`
	// Sheets are the workbook's tabs.
	Sheets []SpreadsheetTab `json:"sheets,omitempty"`
}

// DeletedSpreadsheet confirms the requested spreadsheet identity.
type DeletedSpreadsheet struct {
	// SpreadsheetID is the document requested for permanent deletion.
	SpreadsheetID string `json:"spreadsheetId"`
}

// GetSpreadsheetOperation reads document metadata without grid values.
type GetSpreadsheetOperation struct{ client *Client }

// Definition returns the generated read contract.
func (GetSpreadsheetOperation) Definition() sdkgo.QueryDefinition { return GetSpreadsheetDefinition }

// GetSpreadsheet binds the read Query to the client.
func (client *Client) GetSpreadsheet() GetSpreadsheetOperation {
	return GetSpreadsheetOperation{client}
}

// Invoke reads the document title and tab identities.
func (operation GetSpreadsheetOperation) Invoke(call sdkgo.Call, input GetSpreadsheetInput) sdkgo.QueryAttempt[SpreadsheetInfo] {
	if !validSpreadsheetID(input.SpreadsheetID) {
		return spreadsheetDocumentQueryBranch(GetSpreadsheetBranchDefect, SpreadsheetInfo{}, sdkgo.FailureValidation, "spreadsheet ID is invalid")
	}
	credential, failure, isTerminal := operation.client.resolveCredential(call.Context, call, "getSpreadsheet")
	if failure != nil {
		if isTerminal {
			return spreadsheetDocumentQueryBranch(GetSpreadsheetBranchDefect, SpreadsheetInfo{}, failure.Kind, failure.Message)
		}
		return sdkgo.NewQueryRetry[SpreadsheetInfo](*failure, 0)
	}
	response, _, err := operation.client.request(call.Context, call, credential, http.MethodGet, input.SpreadsheetID, "", nil, nil)
	if result, hasFailure := spreadsheetDocumentQueryFailure[SpreadsheetInfo](response, err, GetSpreadsheetBranchNotFound, GetSpreadsheetBranchProviderRejected, GetSpreadsheetBranchInvalidResponse, "getSpreadsheet"); hasFailure {
		return result
	}
	info, isValid := decodeSpreadsheetInfo(response.body)
	if !isValid || info.SpreadsheetID != input.SpreadsheetID {
		return spreadsheetDocumentQueryBranch(GetSpreadsheetBranchInvalidResponse, SpreadsheetInfo{}, sdkgo.FailureProtocol, "Google returned invalid spreadsheet metadata")
	}
	return sdkgo.NewQueryBranch(GetSpreadsheetBranchFound, info, nil, sdkgo.Receipt{})
}

// CreateSpreadsheetOperation creates a spreadsheet with a bounded send budget.
type CreateSpreadsheetOperation struct{ client *Client }

// Definition returns the generated create contract.
func (CreateSpreadsheetOperation) Definition() sdkgo.MutationDefinition {
	return CreateSpreadsheetDefinition
}

// IdempotencyKey identifies one logical create, though Google has no wire deduplication.
func (CreateSpreadsheetOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSpreadsheetInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// CreateSpreadsheet binds the create Mutation to the client.
func (client *Client) CreateSpreadsheet() CreateSpreadsheetOperation {
	return CreateSpreadsheetOperation{client}
}

// Invoke creates one document per fenced dispatch and stops at the send cap.
func (operation CreateSpreadsheetOperation) Invoke(call sdkgo.Call, input CreateSpreadsheetInput) sdkgo.MutationAttempt[SpreadsheetInfo] {
	if strings.TrimSpace(input.Title) == "" || len(input.Title) > 255 || strings.ContainsAny(input.Title, "\r\n") {
		return spreadsheetDocumentMutationBranch(CreateSpreadsheetBranchDefect, SpreadsheetInfo{}, sdkgo.FailureValidation, "spreadsheet title is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateSpreadsheetDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return spreadsheetDocumentMutationBranch(CreateSpreadsheetBranchDefect, SpreadsheetInfo{}, sdkgo.FailureLocalDefect, "spreadsheet create checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return spreadsheetDocumentMutationBranch(CreateSpreadsheetBranchDefect, SpreadsheetInfo{}, sdkgo.FailureLocalDefect, "spreadsheet create checkpoint is invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return spreadsheetDocumentMutationBranch(CreateSpreadsheetBranchUncertain, SpreadsheetInfo{}, sdkgo.FailureAvailability, "spreadsheet create may have succeeded but no document ID was returned before the send cap")
	}
	credential, failure, isTerminal := operation.client.resolveCredential(call.Context, call, "createSpreadsheet")
	if failure != nil {
		if isTerminal {
			return spreadsheetDocumentMutationBranch(CreateSpreadsheetBranchDefect, SpreadsheetInfo{}, failure.Kind, failure.Message)
		}
		return sdkgo.NewMutationRetry[SpreadsheetInfo](*failure, 0)
	}
	var response requestResult
	var requestError error
	if err := fence.Dispatch(func(dispatchContext context.Context) error {
		response, _, requestError = operation.client.requestURL(dispatchContext, call, credential, http.MethodPost, strings.TrimRight(operation.client.endpoint.String(), "/")+"/spreadsheets", nil, map[string]any{"properties": map[string]string{"title": input.Title}})
		return nil
	}); err != nil {
		return sdkgo.NewMutationRetry[SpreadsheetInfo](sheetFailure(sdkgo.FailureAvailability, "createSpreadsheet", "spreadsheet create could not be dispatched safely"), 0)
	}
	if requestError != nil {
		return sdkgo.NewMutationRetry[SpreadsheetInfo](sheetFailure(sdkgo.FailureTransport, "createSpreadsheet", "spreadsheet create request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[SpreadsheetInfo](sheetFailure(sdkgo.FailureAvailability, "createSpreadsheet", "spreadsheet refusal checkpoint failed"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[SpreadsheetInfo](sheetFailure(kind, "createSpreadsheet", "Google temporarily refused spreadsheet creation"), 0)
	}
	if response.status >= 500 {
		return sdkgo.NewMutationRetry[SpreadsheetInfo](sheetFailure(sdkgo.FailureAvailability, "createSpreadsheet", "spreadsheet create outcome is unknown"), 0)
	}
	if response.status != http.StatusOK {
		return spreadsheetDocumentMutationBranch(CreateSpreadsheetBranchProviderRejected, SpreadsheetInfo{}, sdkgo.FailureProviderRejection, "Google rejected spreadsheet creation with HTTP "+strconv.Itoa(response.status))
	}
	info, isValid := decodeSpreadsheetInfo(response.body)
	if !isValid {
		return sdkgo.NewMutationRetry[SpreadsheetInfo](sheetFailure(sdkgo.FailureProtocol, "createSpreadsheet", "Google accepted spreadsheet creation but returned invalid metadata"), 0)
	}
	if err := fence.RecordProviderID(info.SpreadsheetID); err != nil {
		return sdkgo.NewMutationRetry[SpreadsheetInfo](sheetFailure(sdkgo.FailureAvailability, "createSpreadsheet", "spreadsheet ID checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationBranch(CreateSpreadsheetBranchCreated, info, nil, spreadsheetDocumentReceipt(call, info.SpreadsheetID))
}

// DeleteSpreadsheetOperation permanently removes one app-accessible file.
type DeleteSpreadsheetOperation struct{ client *Client }

// Definition returns the generated deletion contract.
func (DeleteSpreadsheetOperation) Definition() sdkgo.MutationDefinition {
	return DeleteSpreadsheetDefinition
}

// IdempotencyKey identifies one logical deletion.
func (DeleteSpreadsheetOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSpreadsheetInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// DeleteSpreadsheet binds the deletion Mutation to the client.
func (client *Client) DeleteSpreadsheet() DeleteSpreadsheetOperation {
	return DeleteSpreadsheetOperation{client}
}

// Invoke deletes a spreadsheet through Drive; an absent file is already deleted.
func (operation DeleteSpreadsheetOperation) Invoke(call sdkgo.Call, input DeleteSpreadsheetInput) sdkgo.MutationAttempt[DeletedSpreadsheet] {
	if !validSpreadsheetID(input.SpreadsheetID) {
		return spreadsheetDocumentMutationBranch(DeleteSpreadsheetBranchDefect, DeletedSpreadsheet{}, sdkgo.FailureValidation, "spreadsheet ID is invalid")
	}
	credential, failure, isTerminal := operation.client.resolveCredential(call.Context, call, "deleteSpreadsheet")
	if failure != nil {
		if isTerminal {
			return spreadsheetDocumentMutationBranch(DeleteSpreadsheetBranchDefect, DeletedSpreadsheet{}, failure.Kind, failure.Message)
		}
		return sdkgo.NewMutationRetry[DeletedSpreadsheet](*failure, 0)
	}
	response, _, err := operation.client.requestURL(call.Context, call, credential, http.MethodDelete, operation.client.driveBaseURL+"/drive/v3/files/"+url.PathEscape(input.SpreadsheetID), nil, nil)
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedSpreadsheet](sheetFailure(sdkgo.FailureTransport, "deleteSpreadsheet", "Drive deletion request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[DeletedSpreadsheet](sheetFailure(sdkgo.FailureRateLimit, "deleteSpreadsheet", "Google rate limited Drive deletion"), 0)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[DeletedSpreadsheet](sheetFailure(sdkgo.FailureAvailability, "deleteSpreadsheet", "Drive deletion outcome is unknown"), 0)
	}
	if response.status == http.StatusOK || response.status == http.StatusNoContent || response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteSpreadsheetBranchDeleted, DeletedSpreadsheet{SpreadsheetID: input.SpreadsheetID}, nil, spreadsheetDocumentReceipt(call, input.SpreadsheetID))
	}
	return spreadsheetDocumentMutationBranch(DeleteSpreadsheetBranchProviderRejected, DeletedSpreadsheet{}, sdkgo.FailureProviderRejection, "Google rejected Drive deletion with HTTP "+strconv.Itoa(response.status))
}

func decodeSpreadsheetInfo(body []byte) (SpreadsheetInfo, bool) {
	var wire struct {
		SpreadsheetID string `json:"spreadsheetId"`
		Properties    struct {
			Title string `json:"title"`
		} `json:"properties"`
		Sheets []struct {
			Properties SpreadsheetTab `json:"properties"`
		} `json:"sheets"`
	}
	if json.Unmarshal(body, &wire) != nil || !validSpreadsheetID(wire.SpreadsheetID) || wire.Properties.Title == "" {
		return SpreadsheetInfo{}, false
	}
	info := SpreadsheetInfo{SpreadsheetID: wire.SpreadsheetID, Title: wire.Properties.Title, Sheets: make([]SpreadsheetTab, 0, len(wire.Sheets))}
	for _, sheet := range wire.Sheets {
		if sheet.Properties.Title == "" || sheet.Properties.SheetID < 0 {
			return SpreadsheetInfo{}, false
		}
		info.Sheets = append(info.Sheets, sheet.Properties)
	}
	return info, true
}
func validSpreadsheetID(id string) bool {
	return len(id) >= 3 && len(id) <= 256 && !strings.ContainsAny(id, "/\\?&#\r\n ")
}
func spreadsheetDocumentQueryFailure[T any](response requestResult, err error, notFound, rejected, invalid sdkgo.BranchID, operation string) (sdkgo.QueryAttempt[T], bool) {
	if err != nil {
		var requestError *providerRequestError
		if errors.As(err, &requestError) && requestError.kind == sdkgo.FailureResponseTooLarge {
			return spreadsheetDocumentQueryBranch(invalid, *new(T), requestError.kind, "Google response exceeded the limit"), true
		}
		return sdkgo.NewQueryRetry[T](sheetFailure(sdkgo.FailureTransport, operation, "Google request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](sheetFailure(sdkgo.FailureRateLimit, operation, "Google rate limited the request"), 0), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[T](sheetFailure(sdkgo.FailureAvailability, operation, "Google is temporarily unavailable"), 0), true
	}
	if response.status == http.StatusNotFound {
		return spreadsheetDocumentQueryBranch(notFound, *new(T), sdkgo.FailureNotFound, "spreadsheet was not found"), true
	}
	if response.status != http.StatusOK {
		return spreadsheetDocumentQueryBranch(rejected, *new(T), sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status)), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}
func spreadsheetDocumentQueryBranch[T any](branch sdkgo.BranchID, output T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sheetFailure(kind, "spreadsheetDocument", message)
	return sdkgo.NewQueryBranch(branch, output, &failure, sdkgo.Receipt{})
}
func spreadsheetDocumentMutationBranch[T any](branch sdkgo.BranchID, output T, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := sheetFailure(kind, "spreadsheetDocument", message)
	return sdkgo.NewMutationBranch(branch, output, &failure, sdkgo.Receipt{})
}
func spreadsheetDocumentReceipt(call sdkgo.Call, id string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, Provider: "google-sheets", ProviderObjectID: id, ObservedAt: time.Now().UTC()}
}
