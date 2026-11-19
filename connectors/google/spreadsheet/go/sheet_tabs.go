// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateSheetInput names a tab in an existing spreadsheet.
type CreateSheetInput struct {
	// SpreadsheetID identifies the document.
	SpreadsheetID string `json:"spreadsheetId"`
	// Title names the new tab.
	Title string `json:"title"`
}

// DeleteSheetInput identifies a tab by Google's stable numeric ID.
type DeleteSheetInput struct {
	// SpreadsheetID identifies the document.
	SpreadsheetID string `json:"spreadsheetId"`
	// SheetID identifies the tab within the document.
	SheetID int64 `json:"sheetId"`
}

// DeletedSheet confirms the removed tab identity.
type DeletedSheet struct {
	// SpreadsheetID identifies the document.
	SpreadsheetID string `json:"spreadsheetId"`
	// SheetID identifies the removed tab.
	SheetID int64 `json:"sheetId"`
}

// ClearValuesInput selects an A1 range to clear.
type ClearValuesInput struct {
	// SpreadsheetID identifies the document.
	SpreadsheetID string `json:"spreadsheetId"`
	// Range is A1 notation, including the tab name.
	Range string `json:"range"`
}

// ClearedValues identifies the range Google cleared.
type ClearedValues struct {
	// SpreadsheetID identifies the document.
	SpreadsheetID string `json:"spreadsheetId"`
	// ClearedRange is the cleared A1 range returned by Google.
	ClearedRange string `json:"clearedRange"`
}

// CreateSheetOperation adds a tab with a stable call-derived ID.
type CreateSheetOperation struct{ client *Client }

// Definition returns the generated tab creation contract.
func (CreateSheetOperation) Definition() sdkgo.MutationDefinition { return CreateSheetDefinition }

// IdempotencyKey identifies one logical tab creation.
func (CreateSheetOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSheetInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// CreateSheet binds tab creation to the client.
func (client *Client) CreateSheet() CreateSheetOperation { return CreateSheetOperation{client} }

// Invoke creates or confirms the tab using the same ID across retries.
func (operation CreateSheetOperation) Invoke(call sdkgo.Call, input CreateSheetInput) sdkgo.MutationAttempt[SpreadsheetTab] {
	if !validSpreadsheetID(input.SpreadsheetID) || !validSheetTitle(input.Title) {
		return spreadsheetDocumentMutationBranch(CreateSheetBranchDefect, SpreadsheetTab{}, sdkgo.FailureValidation, "spreadsheet ID or tab title is invalid")
	}
	credential, failure, isTerminal := operation.client.resolveCredential(call.Context, call, "createSheet")
	if failure != nil {
		if isTerminal {
			return spreadsheetDocumentMutationBranch(CreateSheetBranchDefect, SpreadsheetTab{}, failure.Kind, failure.Message)
		}
		return sdkgo.NewMutationRetry[SpreadsheetTab](*failure, 0)
	}
	sheetID := stableSheetID(call.ID)
	payload := map[string]any{"requests": []any{map[string]any{"addSheet": map[string]any{"properties": map[string]any{"sheetId": sheetID, "title": input.Title}}}}}
	response, _, err := operation.client.request(call.Context, call, credential, http.MethodPost, input.SpreadsheetID, ":batchUpdate", nil, payload)
	if retry, hasRetry := sheetMutationRetry[SpreadsheetTab](response, err, "createSheet"); hasRetry {
		return retry
	}
	if response.status == http.StatusBadRequest || response.status == http.StatusConflict {
		metadata, _, readError := operation.client.request(call.Context, call, credential, http.MethodGet, input.SpreadsheetID, "", nil, nil)
		if retry, hasRetry := sheetMutationRetry[SpreadsheetTab](metadata, readError, "createSheet read-back"); hasRetry {
			return retry
		}
		if metadata.status == http.StatusOK {
			info, isValid := decodeSpreadsheetInfo(metadata.body)
			if !isValid {
				return spreadsheetDocumentMutationBranch(CreateSheetBranchInvalidResponse, SpreadsheetTab{}, sdkgo.FailureProtocol, "Google returned invalid tab metadata")
			}
			for _, tab := range info.Sheets {
				if tab.SheetID == sheetID && tab.Title == input.Title {
					return sdkgo.NewMutationBranch(CreateSheetBranchCreated, tab, nil, spreadsheetDocumentReceipt(call, input.SpreadsheetID+"#"+strconv.FormatInt(sheetID, 10)))
				}
			}
		}
	}
	if response.status == http.StatusNotFound {
		return spreadsheetDocumentMutationBranch(CreateSheetBranchNotFound, SpreadsheetTab{}, sdkgo.FailureNotFound, "spreadsheet was not found")
	}
	if response.status != http.StatusOK {
		return spreadsheetDocumentMutationBranch(CreateSheetBranchProviderRejected, SpreadsheetTab{}, sdkgo.FailureProviderRejection, "Google rejected tab creation with HTTP "+strconv.Itoa(response.status))
	}
	var wire struct {
		Replies []struct {
			AddSheet struct {
				Properties SpreadsheetTab `json:"properties"`
			} `json:"addSheet"`
		} `json:"replies"`
	}
	if json.Unmarshal(response.body, &wire) != nil || len(wire.Replies) != 1 || wire.Replies[0].AddSheet.Properties.SheetID != sheetID || wire.Replies[0].AddSheet.Properties.Title != input.Title {
		return spreadsheetDocumentMutationBranch(CreateSheetBranchInvalidResponse, SpreadsheetTab{}, sdkgo.FailureProtocol, "Google returned invalid created tab metadata")
	}
	return sdkgo.NewMutationBranch(CreateSheetBranchCreated, wire.Replies[0].AddSheet.Properties, nil, spreadsheetDocumentReceipt(call, input.SpreadsheetID+"#"+strconv.FormatInt(sheetID, 10)))
}

// DeleteSheetOperation removes a tab by stable numeric ID.
type DeleteSheetOperation struct{ client *Client }

// Definition returns the generated tab deletion contract.
func (DeleteSheetOperation) Definition() sdkgo.MutationDefinition { return DeleteSheetDefinition }

// IdempotencyKey identifies one logical tab deletion.
func (DeleteSheetOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSheetInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// DeleteSheet binds tab deletion to the client.
func (client *Client) DeleteSheet() DeleteSheetOperation { return DeleteSheetOperation{client} }

// Invoke deletes a tab, confirming an already absent tab on retry.
func (operation DeleteSheetOperation) Invoke(call sdkgo.Call, input DeleteSheetInput) sdkgo.MutationAttempt[DeletedSheet] {
	if !validSpreadsheetID(input.SpreadsheetID) || input.SheetID < 0 || input.SheetID > 2147483647 {
		return spreadsheetDocumentMutationBranch(DeleteSheetBranchDefect, DeletedSheet{}, sdkgo.FailureValidation, "spreadsheet or tab ID is invalid")
	}
	credential, failure, isTerminal := operation.client.resolveCredential(call.Context, call, "deleteSheet")
	if failure != nil {
		if isTerminal {
			return spreadsheetDocumentMutationBranch(DeleteSheetBranchDefect, DeletedSheet{}, failure.Kind, failure.Message)
		}
		return sdkgo.NewMutationRetry[DeletedSheet](*failure, 0)
	}
	payload := map[string]any{"requests": []any{map[string]any{"deleteSheet": map[string]int64{"sheetId": input.SheetID}}}}
	response, _, err := operation.client.request(call.Context, call, credential, http.MethodPost, input.SpreadsheetID, ":batchUpdate", nil, payload)
	if retry, hasRetry := sheetMutationRetry[DeletedSheet](response, err, "deleteSheet"); hasRetry {
		return retry
	}
	if response.status == http.StatusBadRequest {
		metadata, _, readError := operation.client.request(call.Context, call, credential, http.MethodGet, input.SpreadsheetID, "", nil, nil)
		if retry, hasRetry := sheetMutationRetry[DeletedSheet](metadata, readError, "deleteSheet read-back"); hasRetry {
			return retry
		}
		if metadata.status == http.StatusOK {
			info, isValid := decodeSpreadsheetInfo(metadata.body)
			if !isValid {
				return sdkgo.NewMutationRetry[DeletedSheet](sheetFailure(sdkgo.FailureProtocol, "deleteSheet", "Google returned invalid tab metadata"), 0)
			}
			for _, tab := range info.Sheets {
				if tab.SheetID == input.SheetID {
					return spreadsheetDocumentMutationBranch(DeleteSheetBranchProviderRejected, DeletedSheet{}, sdkgo.FailureProviderRejection, "Google rejected tab deletion")
				}
			}
			return sdkgo.NewMutationBranch(DeleteSheetBranchDeleted, DeletedSheet(input), nil, spreadsheetDocumentReceipt(call, input.SpreadsheetID+"#"+strconv.FormatInt(input.SheetID, 10)))
		}
	}
	if response.status == http.StatusNotFound {
		return spreadsheetDocumentMutationBranch(DeleteSheetBranchNotFound, DeletedSheet{}, sdkgo.FailureNotFound, "spreadsheet was not found")
	}
	if response.status != http.StatusOK {
		return spreadsheetDocumentMutationBranch(DeleteSheetBranchProviderRejected, DeletedSheet{}, sdkgo.FailureProviderRejection, "Google rejected tab deletion with HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(DeleteSheetBranchDeleted, DeletedSheet(input), nil, spreadsheetDocumentReceipt(call, input.SpreadsheetID+"#"+strconv.FormatInt(input.SheetID, 10)))
}

// ClearValuesOperation removes cell values while preserving sheet structure.
type ClearValuesOperation struct{ client *Client }

// Definition returns the generated clear contract.
func (ClearValuesOperation) Definition() sdkgo.MutationDefinition { return ClearValuesDefinition }

// IdempotencyKey identifies one logical clear.
func (ClearValuesOperation) IdempotencyKey(callID sdkgo.CallID, _ ClearValuesInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// ClearValues binds range clearing to the client.
func (client *Client) ClearValues() ClearValuesOperation { return ClearValuesOperation{client} }

// Invoke clears an A1 range; repeating the same clear is harmless.
func (operation ClearValuesOperation) Invoke(call sdkgo.Call, input ClearValuesInput) sdkgo.MutationAttempt[ClearedValues] {
	if !validSpreadsheetID(input.SpreadsheetID) || strings.TrimSpace(input.Range) == "" || len(input.Range) > 512 || strings.ContainsAny(input.Range, "\r\n") {
		return spreadsheetDocumentMutationBranch(ClearValuesBranchDefect, ClearedValues{}, sdkgo.FailureValidation, "spreadsheet ID or range is invalid")
	}
	credential, failure, isTerminal := operation.client.resolveCredential(call.Context, call, "clearValues")
	if failure != nil {
		if isTerminal {
			return spreadsheetDocumentMutationBranch(ClearValuesBranchDefect, ClearedValues{}, failure.Kind, failure.Message)
		}
		return sdkgo.NewMutationRetry[ClearedValues](*failure, 0)
	}
	response, _, err := operation.client.request(call.Context, call, credential, http.MethodPost, input.SpreadsheetID, "/values/"+url.PathEscape(input.Range)+":clear", nil, map[string]any{})
	if retry, hasRetry := sheetMutationRetry[ClearedValues](response, err, "clearValues"); hasRetry {
		return retry
	}
	if response.status == http.StatusNotFound {
		return spreadsheetDocumentMutationBranch(ClearValuesBranchNotFound, ClearedValues{}, sdkgo.FailureNotFound, "spreadsheet or range was not found")
	}
	if response.status != http.StatusOK {
		return spreadsheetDocumentMutationBranch(ClearValuesBranchProviderRejected, ClearedValues{}, sdkgo.FailureProviderRejection, "Google rejected range clear with HTTP "+strconv.Itoa(response.status))
	}
	var output ClearedValues
	if json.Unmarshal(response.body, &output) != nil || output.SpreadsheetID != input.SpreadsheetID || output.ClearedRange == "" {
		return spreadsheetDocumentMutationBranch(ClearValuesBranchInvalidResponse, ClearedValues{}, sdkgo.FailureProtocol, "Google returned invalid clear metadata")
	}
	return sdkgo.NewMutationBranch(ClearValuesBranchCleared, output, nil, spreadsheetDocumentReceipt(call, input.SpreadsheetID))
}

func stableSheetID(callID sdkgo.CallID) int64 {
	sum := sha256.Sum256([]byte(callID))
	return int64(binary.BigEndian.Uint32(sum[:4]) & 0x7fffffff)
}

func validSheetTitle(title string) bool {
	return strings.TrimSpace(title) != "" && len(title) <= 100 && !strings.ContainsAny(title, "[]:*?/\\\r\n") && !strings.HasPrefix(title, "'") && !strings.HasSuffix(title, "'")
}

func sheetMutationRetry[T any](response requestResult, err error, operation string) (sdkgo.MutationAttempt[T], bool) {
	if err != nil {
		return sdkgo.NewMutationRetry[T](sheetFailure(sdkgo.FailureTransport, operation, "Google request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[T](sheetFailure(sdkgo.FailureRateLimit, operation, "Google rate limited the request"), 0), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[T](sheetFailure(sdkgo.FailureAvailability, operation, "Google request outcome is unknown"), 0), true
	}
	return sdkgo.MutationAttempt[T]{}, false
}
