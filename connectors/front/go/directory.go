// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// DirectoryRecord contains a Front resource ID and display name.
type DirectoryRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DirectoryPage contains accessible records and an optional continuation token.
type DirectoryPage struct {
	Records       []DirectoryRecord `json:"records"`
	NextPageToken string            `json:"nextPageToken,omitempty"`
}

// ListInboxesInput requests accessible inboxes.
type ListInboxesInput struct{}

// ListTagsInput selects one bounded tag page.
type ListTagsInput struct {
	PageSize  int    `json:"pageSize,omitempty"`
	PageToken string `json:"pageToken,omitempty"`
}

// ListTeammatesInput requests accessible teammates.
type ListTeammatesInput struct{}

// ListInboxesOperation lists accessible inboxes.
type ListInboxesOperation struct{ client *Client }

// ListTagsOperation lists accessible tags.
type ListTagsOperation struct{ client *Client }

// ListTeammatesOperation lists accessible teammates.
type ListTeammatesOperation struct{ client *Client }

// ListInboxes binds the query to this client.
func (client *Client) ListInboxes() ListInboxesOperation { return ListInboxesOperation{client} }

// ListTags binds the query to this client.
func (client *Client) ListTags() ListTagsOperation { return ListTagsOperation{client} }

// ListTeammates binds the query to this client.
func (client *Client) ListTeammates() ListTeammatesOperation { return ListTeammatesOperation{client} }

// Definition returns the manifest-generated operation contract.
func (ListInboxesOperation) Definition() sdkgo.QueryDefinition { return ListInboxesDefinition }

// Definition returns the manifest-generated operation contract.
func (ListTagsOperation) Definition() sdkgo.QueryDefinition { return ListTagsDefinition }

// Definition returns the manifest-generated operation contract.
func (ListTeammatesOperation) Definition() sdkgo.QueryDefinition { return ListTeammatesDefinition }

// Invoke lists inboxes visible to the API token.
func (operation ListInboxesOperation) Invoke(call sdkgo.Call, _ ListInboxesInput) sdkgo.QueryAttempt[DirectoryPage] {
	return invokeDirectoryList(call, operation.client, ListInboxesDefinition.Operation.OperationID, "/inboxes", nil, inboxIDPattern, directoryBranches{ListInboxesBranchListed, ListInboxesBranchProviderRejected, ListInboxesBranchInvalidResponse, ListInboxesBranchDefect})
}

// Invoke lists one page of tags visible to the API token.
func (operation ListTagsOperation) Invoke(call sdkgo.Call, input ListTagsInput) sdkgo.QueryAttempt[DirectoryPage] {
	if input.PageSize < 0 || input.PageSize > 100 || (input.PageToken != "" && !pageTokenPattern.MatchString(input.PageToken)) {
		return sdkgo.NewQueryBranch(ListTagsBranchDefect, DirectoryPage{Records: []DirectoryRecord{}}, frontFailurePointer(sdkgo.FailureValidation, ListTagsDefinition.Operation.OperationID, "pageSize or pageToken is invalid"), sdkgo.Receipt{})
	}
	parameters := url.Values{"limit": {strconv.Itoa(pageSizeOrDefault(input.PageSize))}}
	if input.PageToken != "" {
		parameters.Set("page_token", input.PageToken)
	}
	return invokeDirectoryList(call, operation.client, ListTagsDefinition.Operation.OperationID, "/tags", parameters, tagIDPattern, directoryBranches{ListTagsBranchListed, ListTagsBranchProviderRejected, ListTagsBranchInvalidResponse, ListTagsBranchDefect})
}

// Invoke lists teammates visible to the API token.
func (operation ListTeammatesOperation) Invoke(call sdkgo.Call, _ ListTeammatesInput) sdkgo.QueryAttempt[DirectoryPage] {
	return invokeDirectoryList(call, operation.client, ListTeammatesDefinition.Operation.OperationID, "/teammates", nil, teammateIDPattern, directoryBranches{ListTeammatesBranchListed, ListTeammatesBranchProviderRejected, ListTeammatesBranchInvalidResponse, ListTeammatesBranchDefect})
}

type directoryBranches struct{ listed, rejected, invalid, defect sdkgo.BranchID }

func invokeDirectoryList(call sdkgo.Call, client *Client, operationID, path string, parameters url.Values, idPattern *regexp.Regexp, branches directoryBranches) sdkgo.QueryAttempt[DirectoryPage] {
	output := DirectoryPage{Records: []DirectoryRecord{}}
	credentials, failure := client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, output, failure, sdkgo.Receipt{})
	}
	result := client.exchange(call.Context, credentials, operationID, frontRequest{method: http.MethodGet, path: path, query: parameters})
	receipt := client.receipt(call, "")
	switch {
	case result.isRetryable():
		return sdkgo.NewQueryRetry[DirectoryPage](result.failure, result.retryAfter)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(branches.defect, output, &result.failure, sdkgo.Receipt{})
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(branches.invalid, output, &result.failure, receipt)
	case result.outcome != exchangeSucceeded:
		return sdkgo.NewQueryBranch(branches.rejected, output, &result.failure, receipt)
	}
	var page frontPageWire[struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}]
	if json.Unmarshal(result.response.body, &page) != nil || page.Results == nil {
		return sdkgo.NewQueryBranch(branches.invalid, output, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid resource list"), receipt)
	}
	for _, item := range page.Results {
		name := item.Name
		if path == "/teammates" {
			name = strings.TrimSpace(item.FirstName + " " + item.LastName)
		}
		if !idPattern.MatchString(item.ID) || name == "" {
			return sdkgo.NewQueryBranch(branches.invalid, DirectoryPage{Records: []DirectoryRecord{}}, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid resource identity"), receipt)
		}
		output.Records = append(output.Records, DirectoryRecord{ID: item.ID, Name: name})
	}
	var err error
	output.NextPageToken, err = client.nextPageToken(page.Pagination.Next, path)
	if err != nil {
		return sdkgo.NewQueryBranch(branches.invalid, DirectoryPage{Records: []DirectoryRecord{}}, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid next-page link"), receipt)
	}
	return sdkgo.NewQueryBranch(branches.listed, output, nil, receipt)
}
