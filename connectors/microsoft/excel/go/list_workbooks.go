// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const listWorkbooksOperationID = "listWorkbooks"

// ListWorkbooksInput selects one drive search page.
type ListWorkbooksInput struct {
	// DriveID is the Graph drive ID, obtained from the workbook picker.
	DriveID string `json:"driveId"`
	// PageToken continues from a preceding ListWorkbooksOutput, or is blank for the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// WorkbookItem identifies a workbook in a drive.
type WorkbookItem struct {
	// ID is the workbook's drive item ID.
	ID string `json:"id"`
	// Name is the workbook filename.
	Name string `json:"name"`
	// DriveID is the Graph drive containing the workbook.
	DriveID string `json:"driveId"`
}

// ListWorkbooksOutput holds one search page and its continuation token.
type ListWorkbooksOutput struct {
	// Workbooks are the Excel workbooks in this page.
	Workbooks []WorkbookItem `json:"workbooks"`
	// NextPageToken continues the drive search; blank ends it.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListWorkbooksOperation implements the listWorkbooks query.
type ListWorkbooksOperation struct{ client *Client }

var listWorkbooksReadBranches = readBranches{
	operationID:      listWorkbooksOperationID,
	notFound:         ListWorkbooksBranchNotFound,
	tooLarge:         ListWorkbooksBranchTooLarge,
	providerRejected: ListWorkbooksBranchProviderRejected,
	invalidResponse:  ListWorkbooksBranchInvalidResponse,
	defect:           ListWorkbooksBranchDefect,
}

// Definition returns the query definition.
func (ListWorkbooksOperation) Definition() sdkgo.QueryDefinition { return ListWorkbooksDefinition }

// Invoke searches a drive for supported Excel workbooks.
func (operation ListWorkbooksOperation) Invoke(call sdkgo.Call, input ListWorkbooksInput) sdkgo.QueryAttempt[ListWorkbooksOutput] {
	if !graphDriveItemIDPattern.MatchString(input.DriveID) || len(input.PageToken) > 4096 || hasControlCharacter(input.PageToken) {
		return sdkgo.NewQueryBranch(ListWorkbooksBranchDefect, ListWorkbooksOutput{}, excelFailurePointer(sdkgo.FailureValidation, listWorkbooksOperationID, "drive ID or page token is invalid"), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, listWorkbooksOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListWorkbooksBranchDefect, ListWorkbooksOutput{}, failure, sdkgo.Receipt{})
	}
	target := graphBaseURL + "/drives/" + url.PathEscape(input.DriveID) + "/root/search(q='.xlsx')?$select=id,name,file,parentReference"
	if input.PageToken != "" {
		target += "&$skiptoken=" + url.QueryEscape(input.PageToken)
	}
	response, outcome := operation.client.sendRead(call, &credential, listWorkbooksReadBranches, graphRequest{method: http.MethodGet, target: target, responseLimit: operation.client.maxResponseBytes})
	if outcome != nil {
		return queryAttemptFromReadOutcome[ListWorkbooksOutput](outcome)
	}
	receipt := operation.client.receipt(call, response.requestID)
	var body struct {
		Value *[]struct {
			ID              string           `json:"id"`
			Name            string           `json:"name"`
			File            *json.RawMessage `json:"file"`
			ParentReference struct {
				DriveID string `json:"driveId"`
			} `json:"parentReference"`
		} `json:"value"`
		NextLink string `json:"@odata.nextLink"`
	}
	if err := json.Unmarshal(response.body, &body); err != nil || body.Value == nil {
		return sdkgo.NewQueryBranch(ListWorkbooksBranchInvalidResponse, ListWorkbooksOutput{}, excelFailurePointer(sdkgo.FailureProtocol, listWorkbooksOperationID, "provider returned an invalid workbook page"), receipt)
	}
	output := ListWorkbooksOutput{Workbooks: make([]WorkbookItem, 0, len(*body.Value))}
	for _, item := range *body.Value {
		if !graphDriveItemIDPattern.MatchString(item.ID) || item.Name == "" || item.ParentReference.DriveID != input.DriveID {
			return sdkgo.NewQueryBranch(ListWorkbooksBranchInvalidResponse, ListWorkbooksOutput{}, excelFailurePointer(sdkgo.FailureProtocol, listWorkbooksOperationID, "provider returned an invalid workbook item"), receipt)
		}
		if item.File != nil && strings.HasSuffix(strings.ToLower(item.Name), ".xlsx") {
			output.Workbooks = append(output.Workbooks, WorkbookItem{ID: item.ID, Name: item.Name, DriveID: input.DriveID})
		}
	}
	if body.NextLink != "" {
		next, err := url.Parse(body.NextLink)
		if err != nil || next.Scheme != "https" || next.Host != graphHost || next.Path != "/v1.0/drives/"+url.PathEscape(input.DriveID)+"/root/search(q='.xlsx')" || next.Query().Get("$skiptoken") == "" {
			return sdkgo.NewQueryBranch(ListWorkbooksBranchInvalidResponse, ListWorkbooksOutput{}, excelFailurePointer(sdkgo.FailureProtocol, listWorkbooksOperationID, "provider returned an invalid next page link"), receipt)
		}
		output.NextPageToken = next.Query().Get("$skiptoken")
	}
	return sdkgo.NewQueryBranch(ListWorkbooksBranchListed, output, nil, receipt)
}
