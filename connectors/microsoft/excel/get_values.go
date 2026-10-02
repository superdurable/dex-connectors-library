// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getValuesOperationID = "getValues"

// GetValuesInput identifies one bounded range of one worksheet.
type GetValuesInput struct {
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
}

// GetValuesOutput holds one range's cells, row by row.
type GetValuesOutput struct {
	// Address is the range as Excel reports it, qualified by its worksheet name, such as Summary!A2:E2.
	Address string `json:"address"`
	// RowCount is the number of rows read.
	RowCount int `json:"rowCount"`
	// ColumnCount is the number of columns read.
	ColumnCount int `json:"columnCount"`
	// Values holds every cell's raw value, row by row: text, a number, a
	// Boolean, or empty text for an empty cell. A date is its serial number.
	Values [][]CellValue `json:"values"`
	// Text holds every cell's text as Excel displays it with its number
	// format, such as 10/1/2026 for a date, row by row.
	Text [][]string `json:"text"`
}

// GetValuesOperation implements the getValues connector operation.
type GetValuesOperation struct{ client *Client }

var getValuesReadBranches = readBranches{
	operationID: getValuesOperationID, notFound: GetValuesBranchNotFound, tooLarge: GetValuesBranchTooLarge,
	providerRejected: GetValuesBranchProviderRejected, invalidResponse: GetValuesBranchInvalidResponse,
	defect: GetValuesBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetValuesOperation) Definition() sdkgo.QueryDefinition { return GetValuesDefinition }

// Invoke reads the range's address, size, raw values, and displayed text in
// one request. An address above the cell limit selects defect without a request.
func (operation GetValuesOperation) Invoke(call sdkgo.Call, input GetValuesInput) sdkgo.QueryAttempt[GetValuesOutput] {
	client := operation.client
	address, err := validateRangeLocation(input.DriveID, input.WorkbookID, input.Worksheet, input.Address, client.maxCells)
	if err != nil {
		return sdkgo.NewQueryBranch(GetValuesBranchDefect, GetValuesOutput{}, excelFailurePointer(sdkgo.FailureValidation, getValuesOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, getValuesOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetValuesBranchDefect, GetValuesOutput{}, failure, sdkgo.Receipt{})
	}
	response, outcome := client.sendRead(call, &credential, getValuesReadBranches, graphRequest{
		method: http.MethodGet, target: rangeURL(input.DriveID, input.WorkbookID, input.Worksheet, address.address) + rangeValuesAndTexts,
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return queryAttemptFromReadOutcome[GetValuesOutput](outcome)
	}
	receipt := client.receipt(call, response.requestID)
	body, err := decodeRange(response.body, true)
	if err == nil && (body.rowCount != address.rowCount || body.columnCount != address.columnCount) {
		err = errors.New("range size differs from the requested address")
	}
	if err != nil {
		return sdkgo.NewQueryBranch(GetValuesBranchInvalidResponse, GetValuesOutput{}, excelFailurePointer(sdkgo.FailureProtocol, getValuesOperationID, "provider returned an invalid range response"), receipt)
	}
	return sdkgo.NewQueryBranch(GetValuesBranchRead, GetValuesOutput{
		Address: body.address, RowCount: int(body.rowCount), ColumnCount: int(body.columnCount),
		Values: body.values, Text: body.texts,
	}, nil, receipt)
}

func validateRangeLocation(driveID, workbookID, worksheet, address string, maxCells int64) (a1Range, error) {
	if err := validateWorkbookLocation(driveID, workbookID); err != nil {
		return a1Range{}, err
	}
	if err := validateWorksheetReference(worksheet); err != nil {
		return a1Range{}, err
	}
	parsed, err := parseA1Range(address)
	if err != nil {
		return a1Range{}, err
	}
	if parsed.cellCount() > maxCells {
		return a1Range{}, errors.New("address covers more cells than the configured cell limit")
	}
	return parsed, nil
}
