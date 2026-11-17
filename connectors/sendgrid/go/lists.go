// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendgrid

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// MarketingList identifies one static SendGrid contact list.
type MarketingList struct {
	// ID is the provider-issued list ID.
	ID string `json:"id"`
	// Name is the list's display name.
	Name string `json:"name"`
	// ContactCount is the provider-reported number of associated contacts.
	ContactCount int `json:"contactCount"`
}

// ListIDInput identifies one static marketing list.
type ListIDInput struct {
	// ID is the provider-issued ID from SendGrid Marketing > Contacts > Lists.
	ID string `json:"id"`
}

// ListListsInput chooses a page of static marketing lists.
type ListListsInput struct {
	// PageSize is 1 to 1000; zero selects 100.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the token from the previous page; blank starts at the beginning.
	PageToken string `json:"pageToken,omitempty"`
}

// ListListsOutput contains one list page and its next-page token.
type ListListsOutput struct {
	// Lists contains the returned static lists.
	Lists []MarketingList `json:"lists"`
	// Count is the provider-reported total across pages.
	Count int `json:"count"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// GetListOperation reads one static marketing list.
type GetListOperation struct{ client *Client }

// Definition returns the generated operation contract.
func (GetListOperation) Definition() sdkgo.QueryDefinition { return GetListDefinition }

// GetList binds the operation to this client.
func (client *Client) GetList() GetListOperation { return GetListOperation{client} }

// Invoke retrieves one list by ID.
func (operation GetListOperation) Invoke(call sdkgo.Call, input ListIDInput) sdkgo.QueryAttempt[MarketingList] {
	branches := queryBranches{found: GetListBranchFound, notFound: GetListBranchNotFound, rejected: GetListBranchProviderRejected, invalid: GetListBranchInvalidResponse, defect: GetListBranchDefect}
	if !isSafeIdentifier(input.ID) {
		return queryBranch("getList", branches.defect, MarketingList{}, sdkgo.FailureValidation, "list ID is invalid")
	}
	return invokeJSONQuery(call, operation.client, "getList", branches, http.MethodGet, "/v3/marketing/lists/"+url.PathEscape(input.ID), nil, nil, decodeMarketingList)
}

// ListListsOperation lists static marketing lists.
type ListListsOperation struct{ client *Client }

// Definition returns the generated operation contract.
func (ListListsOperation) Definition() sdkgo.QueryDefinition { return ListListsDefinition }

// ListLists binds the operation to this client.
func (client *Client) ListLists() ListListsOperation { return ListListsOperation{client} }

// Invoke retrieves one bounded list page.
func (operation ListListsOperation) Invoke(call sdkgo.Call, input ListListsInput) sdkgo.QueryAttempt[ListListsOutput] {
	branches := queryBranches{found: ListListsBranchListed, rejected: ListListsBranchProviderRejected, invalid: ListListsBranchInvalidResponse, defect: ListListsBranchDefect}
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	if input.PageSize < 1 || input.PageSize > 1000 || len(input.PageToken) > 2048 {
		return queryBranch("listLists", branches.defect, ListListsOutput{}, sdkgo.FailureValidation, "page size or token is invalid")
	}
	query := url.Values{"page_size": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	return invokeJSONQuery(call, operation.client, "listLists", branches, http.MethodGet, "/v3/marketing/lists", query, nil, decodeMarketingLists)
}

// UpdateListInput names one list and its replacement display name.
type UpdateListInput struct {
	// ID is the provider-issued list ID.
	ID string `json:"id"`
	// Name is the new nonblank display name.
	Name string `json:"name"`
}

// UpdateListOperation renames one static marketing list.
type UpdateListOperation struct{ client *Client }

// Definition returns the generated operation contract.
func (UpdateListOperation) Definition() sdkgo.MutationDefinition { return UpdateListDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (UpdateListOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// UpdateList binds the operation to this client.
func (client *Client) UpdateList() UpdateListOperation { return UpdateListOperation{client} }

// Invoke replaces a list's name, which is repeat-safe.
func (operation UpdateListOperation) Invoke(call sdkgo.Call, input UpdateListInput) sdkgo.MutationAttempt[MarketingList] {
	branches := mutationBranches{completed: UpdateListBranchUpdated, notFound: UpdateListBranchNotFound, rejected: UpdateListBranchProviderRejected, invalid: UpdateListBranchInvalidResponse, defect: UpdateListBranchDefect}
	if !isSafeIdentifier(input.ID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 100 {
		return mutationBranch("updateList", branches.defect, MarketingList{}, sdkgo.FailureValidation, "list ID or name is invalid")
	}
	payload, _ := json.Marshal(map[string]string{"name": input.Name})
	return invokeSafeMutation(call, operation.client, "updateList", branches, http.MethodPatch, "/v3/marketing/lists/"+url.PathEscape(input.ID), payload, http.StatusOK, decodeMarketingList)
}

// DeletedListOutput identifies a removed static list.
type DeletedListOutput struct {
	// ID is the deleted provider-issued list ID.
	ID string `json:"id"`
}

// DeleteListOperation removes one static list without deleting its contacts.
type DeleteListOperation struct{ client *Client }

// Definition returns the generated operation contract.
func (DeleteListOperation) Definition() sdkgo.MutationDefinition { return DeleteListDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (DeleteListOperation) IdempotencyKey(callID sdkgo.CallID, _ ListIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// DeleteList binds the operation to this client.
func (client *Client) DeleteList() DeleteListOperation { return DeleteListOperation{client} }

// Invoke deletes only the list, leaving its contacts in the account.
func (operation DeleteListOperation) Invoke(call sdkgo.Call, input ListIDInput) sdkgo.MutationAttempt[DeletedListOutput] {
	branches := mutationBranches{completed: DeleteListBranchDeleted, notFound: DeleteListBranchNotFound, rejected: DeleteListBranchProviderRejected, invalid: DeleteListBranchInvalidResponse, defect: DeleteListBranchDefect}
	if !isSafeIdentifier(input.ID) {
		return mutationBranch("deleteList", branches.defect, DeletedListOutput{}, sdkgo.FailureValidation, "list ID is invalid")
	}
	return invokeSafeMutation(call, operation.client, "deleteList", branches, http.MethodDelete, "/v3/marketing/lists/"+url.PathEscape(input.ID), nil, http.StatusNoContent, func([]byte) (DeletedListOutput, error) { return DeletedListOutput{ID: input.ID}, nil })
}

func decodeMarketingList(body []byte) (MarketingList, error) {
	var wire struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		ContactCount int    `json:"contact_count"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ID == "" || wire.Name == "" {
		return MarketingList{}, errors.New("list response is incomplete")
	}
	return MarketingList{ID: wire.ID, Name: wire.Name, ContactCount: wire.ContactCount}, nil
}

func decodeMarketingLists(body []byte) (ListListsOutput, error) {
	var wire struct {
		Result   []json.RawMessage `json:"result"`
		Metadata struct {
			Count int    `json:"count"`
			Next  string `json:"next"`
		} `json:"_metadata"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Result == nil {
		return ListListsOutput{}, errors.New("list page is missing")
	}
	output := ListListsOutput{Lists: make([]MarketingList, 0, len(wire.Result)), Count: wire.Metadata.Count}
	for _, rawList := range wire.Result {
		list, err := decodeMarketingList(rawList)
		if err != nil {
			return ListListsOutput{}, err
		}
		output.Lists = append(output.Lists, list)
	}
	if wire.Metadata.Next != "" {
		nextURL, err := url.Parse(wire.Metadata.Next)
		if err != nil || nextURL.Scheme != "https" && nextURL.Scheme != "http" {
			return ListListsOutput{}, errors.New("next list page is invalid")
		}
		output.NextPageToken = nextURL.Query().Get("page_token")
		if output.NextPageToken == "" {
			return ListListsOutput{}, errors.New("next list page token is missing")
		}
	}
	return output, nil
}
