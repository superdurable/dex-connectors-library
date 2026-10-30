// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const updateValuesOperationID = "updateValues"

// UpdateValuesInput is the literal content of one bounded range.
type UpdateValuesInput struct {
	// DriveID is the Microsoft Graph drive ID of the OneDrive or SharePoint
	// document library that holds the workbook, as the workbookPicker unit stores it.
	DriveID string `json:"driveId"`
	// WorkbookID is the workbook's drive item ID, as the workbookPicker unit stores it.
	WorkbookID string `json:"workbookId"`
	// Worksheet is the worksheet's ID in braces, as the worksheetPicker unit
	// stores it, or its name, such as Summary. An ID keeps working after a rename.
	Worksheet string `json:"worksheet"`
	// Address is one cell or rectangular range in A1 form, such as A2:E2,
	// without a worksheet name, $ markers, or whole columns or rows. It may
	// cover at most the configured maxCells.
	Address string `json:"address"`
	// Values holds every cell of the range, row by row, with exactly the
	// address's number of rows and columns. Every value must be present; use
	// EmptyCellValue to clear a cell.
	Values [][]CellValue `json:"values"`
}

// UpdateValuesOutput identifies the written range.
type UpdateValuesOutput struct {
	// Address is the range as Excel reports it, such as Summary!A2:E2, or the
	// requested address when Excel's response could not be read.
	Address string `json:"address"`
	// RowCount is the number of rows written.
	RowCount int `json:"rowCount"`
	// ColumnCount is the number of columns written.
	ColumnCount int `json:"columnCount"`
}

// UpdateValuesOperation implements the updateValues connector operation.
type UpdateValuesOperation struct{ client *Client }

type updateValuesRequestBody struct {
	Values [][]any `json:"values"`
}

// Definition returns the immutable connector operation definition.
func (UpdateValuesOperation) Definition() sdkgo.MutationDefinition { return UpdateValuesDefinition }

// IdempotencyKey returns the Call ID. Excel has no idempotency key; absolute
// values make every repeat of the same Step execution write the same content.
func (UpdateValuesOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateValuesInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke writes the values with one PATCH of the range. Because the values are
// absolute, a transport failure or an ambiguous server error is retried rather
// than reported as uncertain: repeating the write leaves the same range.
func (operation UpdateValuesOperation) Invoke(call sdkgo.Call, input UpdateValuesInput) sdkgo.MutationAttempt[UpdateValuesOutput] {
	client := operation.client
	address, body, err := validateUpdateValuesInput(input, client.maxCells)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateValuesBranchDefect, UpdateValuesOutput{}, excelFailurePointer(sdkgo.FailureValidation, updateValuesOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, updateValuesOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateValuesBranchDefect, UpdateValuesOutput{}, failure, sdkgo.Receipt{})
	}
	response, err := client.sendRequest(call, &credential, graphRequest{
		method: http.MethodPatch, target: rangeURL(input.DriveID, input.WorkbookID, input.Worksheet, address.address),
		body: body, responseLimit: client.maxResponseBytes,
	})
	receipt := client.receipt(call, response.requestID)
	var transportErr *graphTransportError
	if errors.As(err, &transportErr) && !transportErr.isRequestBuilt {
		return sdkgo.NewMutationBranch(UpdateValuesBranchDefect, UpdateValuesOutput{}, excelFailurePointer(sdkgo.FailureLocalDefect, updateValuesOperationID, err.Error()), receipt)
	}
	if err != nil {
		return sdkgo.NewMutationRetry[UpdateValuesOutput](excelFailure(sdkgo.FailureTransport, updateValuesOperationID, "provider is unavailable; the same values are written again"), 0)
	}
	if isSuccessStatus(response.status) {
		output := UpdateValuesOutput{Address: address.address, RowCount: int(address.rowCount), ColumnCount: int(address.columnCount)}
		var written struct {
			Address string `json:"address"`
		}
		if json.Unmarshal(response.body, &written) == nil && written.Address != "" {
			output.Address = written.Address
		}
		return sdkgo.NewMutationBranch(UpdateValuesBranchUpdated, output, nil, receipt)
	}
	tokens := providerhttp.ReadErrorTokens(response.body, graphErrorTokenPointers)
	kind := statusFailureKind(response.status, tokens)
	switch classifyGraphError(response.status, tokens) {
	case graphErrorRetryable:
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return sdkgo.NewMutationRetry[UpdateValuesOutput](excelFailure(kind, updateValuesOperationID, "provider temporarily rejected the write"), delay)
	case graphErrorNotFound:
		return sdkgo.NewMutationBranch(UpdateValuesBranchNotFound, UpdateValuesOutput{}, excelFailurePointer(sdkgo.FailureNotFound, updateValuesOperationID, "workbook or worksheet was not found or is not visible to the connection"), receipt)
	default:
		return sdkgo.NewMutationBranch(UpdateValuesBranchProviderRejected, UpdateValuesOutput{}, excelFailurePointer(kind, updateValuesOperationID, "provider rejected the write"), receipt)
	}
}

// validateUpdateValuesInput returns the parsed address and the encoded PATCH body.
func validateUpdateValuesInput(input UpdateValuesInput, maxCells int64) (a1Range, []byte, error) {
	address, err := validateRangeLocation(input.DriveID, input.WorkbookID, input.Worksheet, input.Address, maxCells)
	if err != nil {
		return a1Range{}, nil, err
	}
	if int64(len(input.Values)) != address.rowCount {
		return a1Range{}, nil, fmt.Errorf("values has %d rows but the address has %d", len(input.Values), address.rowCount)
	}
	wireRows := make([][]any, len(input.Values))
	for rowIndex, row := range input.Values {
		if int64(len(row)) != address.columnCount {
			return a1Range{}, nil, fmt.Errorf("values row %d has %d cells but the address has %d columns", rowIndex+1, len(row), address.columnCount)
		}
		wireRows[rowIndex] = make([]any, len(row))
		for columnIndex, value := range row {
			wireValue, err := value.wireValue()
			if err != nil {
				return a1Range{}, nil, fmt.Errorf("values row %d cell %d: %w", rowIndex+1, columnIndex+1, err)
			}
			wireRows[rowIndex][columnIndex] = wireValue
		}
	}
	body, err := json.Marshal(updateValuesRequestBody{Values: wireRows})
	if err != nil {
		return a1Range{}, nil, errors.New("values could not be encoded")
	}
	return address, body, nil
}
