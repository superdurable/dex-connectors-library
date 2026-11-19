// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const listWorksheetsOperationID = "listWorksheets"

// ListWorksheetsInput identifies a workbook by its drive and item IDs.
type ListWorksheetsInput struct {
	// DriveID is the Graph drive that holds the workbook.
	DriveID string `json:"driveId"`
	// WorkbookID is the workbook's drive item ID.
	WorkbookID string `json:"workbookId"`
}

// Worksheet describes a workbook worksheet.
type Worksheet struct {
	// ID is the stable Graph worksheet ID.
	ID string `json:"id"`
	// Name is the displayed worksheet name.
	Name string `json:"name"`
	// Position is the zero-based sheet position.
	Position int `json:"position"`
	// Visibility is Visible, Hidden, or VeryHidden.
	Visibility string `json:"visibility"`
}

// ListWorksheetsOutput holds the workbook worksheets.
type ListWorksheetsOutput struct {
	// Worksheets are in workbook order.
	Worksheets []Worksheet `json:"worksheets"`
}

// ListWorksheetsOperation implements the listWorksheets query.
type ListWorksheetsOperation struct{ client *Client }

var listWorksheetsReadBranches = readBranches{
	operationID:      listWorksheetsOperationID,
	notFound:         ListWorksheetsBranchNotFound,
	tooLarge:         ListWorksheetsBranchTooLarge,
	providerRejected: ListWorksheetsBranchProviderRejected,
	invalidResponse:  ListWorksheetsBranchInvalidResponse,
	defect:           ListWorksheetsBranchDefect,
}

// Definition returns the query definition.
func (ListWorksheetsOperation) Definition() sdkgo.QueryDefinition { return ListWorksheetsDefinition }

// Invoke lists and validates the worksheets in one workbook.
func (operation ListWorksheetsOperation) Invoke(call sdkgo.Call, input ListWorksheetsInput) sdkgo.QueryAttempt[ListWorksheetsOutput] {
	if err := validateWorkbookLocation(input.DriveID, input.WorkbookID); err != nil {
		return sdkgo.NewQueryBranch(ListWorksheetsBranchDefect, ListWorksheetsOutput{}, excelFailurePointer(sdkgo.FailureValidation, listWorksheetsOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, listWorksheetsOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListWorksheetsBranchDefect, ListWorksheetsOutput{}, failure, sdkgo.Receipt{})
	}
	response, outcome := operation.client.sendRead(call, &credential, listWorksheetsReadBranches, graphRequest{
		method: http.MethodGet, target: workbookURL(input.DriveID, input.WorkbookID) + "/worksheets?$select=id,name,position,visibility", responseLimit: operation.client.maxResponseBytes,
	})
	if outcome != nil {
		return queryAttemptFromReadOutcome[ListWorksheetsOutput](outcome)
	}
	receipt := operation.client.receipt(call, response.requestID)
	var body struct {
		Value    *[]Worksheet `json:"value"`
		NextLink string       `json:"@odata.nextLink"`
	}
	if err := json.Unmarshal(response.body, &body); err != nil || body.Value == nil || body.NextLink != "" {
		return sdkgo.NewQueryBranch(ListWorksheetsBranchInvalidResponse, ListWorksheetsOutput{}, excelFailurePointer(sdkgo.FailureProtocol, listWorksheetsOperationID, "provider returned an invalid or incomplete worksheet list"), receipt)
	}
	for _, worksheet := range *body.Value {
		if worksheet.ID == "" || validateWorksheetReference(worksheet.Name) != nil || worksheet.Position < 0 {
			return sdkgo.NewQueryBranch(ListWorksheetsBranchInvalidResponse, ListWorksheetsOutput{}, excelFailurePointer(sdkgo.FailureProtocol, listWorksheetsOperationID, "provider returned an invalid worksheet"), receipt)
		}
	}
	return sdkgo.NewQueryBranch(ListWorksheetsBranchListed, ListWorksheetsOutput{Worksheets: *body.Value}, nil, receipt)
}
