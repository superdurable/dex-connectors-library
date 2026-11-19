// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListListsInput selects one offset-based page of email lists.
type ListListsInput struct {
	// Offset is the zero-based number of lists to skip.
	Offset int `json:"offset,omitempty"`
	// Limit is the maximum list count; zero uses 20 and 100 is the maximum.
	Limit int `json:"limit,omitempty"`
}

// GetListInput selects a list by its numeric ActiveCampaign ID.
type GetListInput struct {
	// ListID is the numeric ID from the Lists page or API.
	ListID string `json:"listId"`
}

// ListMetadata contains an email list's identity and subscriber counts.
type ListMetadata struct {
	// ID is the stable provider list ID.
	ID string `json:"id"`
	// Name is the list's display name.
	Name string `json:"name"`
	// ActiveSubscribers counts contacts with active subscriptions; zero for getList, which omits this counter.
	ActiveSubscribers int `json:"activeSubscribers"`
	// NonDeletedSubscribers counts subscriptions in all non-deleted states; zero for getList, which omits this counter.
	NonDeletedSubscribers int `json:"nonDeletedSubscribers"`
}

// ListsPage is one offset-based page of email lists.
type ListsPage struct {
	// Lists contains the selected email lists.
	Lists []ListMetadata `json:"lists"`
	// Offset is the requested zero-based offset.
	Offset int `json:"offset"`
	// Limit is the requested maximum count.
	Limit int `json:"limit"`
	// Total is the provider's matching list count.
	Total int `json:"total"`
}

// ListListsOperation retrieves one page of list metadata.
type ListListsOperation struct{ client *Client }

// GetListOperation retrieves one list by ID.
type GetListOperation struct{ client *Client }

// ListLists binds the list Query to this client.
func (client *Client) ListLists() ListListsOperation { return ListListsOperation{client} }

// GetList binds the single-list Query to this client.
func (client *Client) GetList() GetListOperation { return GetListOperation{client} }

// Definition returns the manifest-generated list contract.
func (ListListsOperation) Definition() sdkgo.QueryDefinition { return ListListsDefinition }

// Definition returns the manifest-generated single-list contract.
func (GetListOperation) Definition() sdkgo.QueryDefinition { return GetListDefinition }

// Invoke reads one page of email list identities and counts.
func (operation ListListsOperation) Invoke(call sdkgo.Call, input ListListsInput) sdkgo.QueryAttempt[ListsPage] {
	branches := queryBranches{found: ListListsBranchListed, rejected: ListListsBranchProviderRejected, invalid: ListListsBranchInvalidResponse, defect: ListListsBranchDefect}
	if input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listLists", branches.defect, ListsPage{}, sdkgo.FailureValidation, "offset or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	return invokeQuery(call, operation.client, "listLists", "/lists?"+query.Encode(), branches, func(body []byte) (ListsPage, error) { return decodeListsPage(body, input) })
}

// Invoke reads one list; the single-list API omits subscriber counters.
func (operation GetListOperation) Invoke(call sdkgo.Call, input GetListInput) sdkgo.QueryAttempt[ListMetadata] {
	branches := queryBranches{found: GetListBranchFound, notFound: GetListBranchNotFound, rejected: GetListBranchProviderRejected, invalid: GetListBranchInvalidResponse, defect: GetListBranchDefect}
	if !numericID.MatchString(input.ListID) {
		return queryBranch("getList", branches.defect, ListMetadata{}, sdkgo.FailureValidation, "list ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getList", "/lists/"+input.ListID, branches, decodeList)
}

type listWire struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	ActiveSubscribers     string `json:"active_subscribers"`
	NonDeletedSubscribers string `json:"non_deleted_subscribers"`
}

func decodeListsPage(body []byte, input ListListsInput) (ListsPage, error) {
	var wire struct {
		Lists []listWire `json:"lists"`
		Meta  struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ListsPage{}, err
	}
	if wire.Lists == nil {
		return ListsPage{}, fmt.Errorf("list collection missing")
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if err != nil || total < 0 {
		return ListsPage{}, fmt.Errorf("list count missing or invalid")
	}
	page := ListsPage{Lists: make([]ListMetadata, 0, len(wire.Lists)), Offset: input.Offset, Limit: input.Limit, Total: total}
	for _, item := range wire.Lists {
		metadata, err := decodeListMetadata(item, true)
		if err != nil {
			return ListsPage{}, err
		}
		page.Lists = append(page.Lists, metadata)
	}
	return page, nil
}

func decodeList(body []byte) (ListMetadata, error) {
	var wire struct {
		List *listWire `json:"list"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ListMetadata{}, err
	}
	if wire.List == nil {
		return ListMetadata{}, fmt.Errorf("list missing")
	}
	return decodeListMetadata(*wire.List, false)
}

func decodeListMetadata(item listWire, hasCounters bool) (ListMetadata, error) {
	if !numericID.MatchString(item.ID) || item.Name == "" {
		return ListMetadata{}, fmt.Errorf("list ID or name missing")
	}
	metadata := ListMetadata{ID: item.ID, Name: item.Name}
	if !hasCounters {
		return metadata, nil
	}
	if item.ActiveSubscribers == "" {
		item.ActiveSubscribers = "0"
	}
	if item.NonDeletedSubscribers == "" {
		item.NonDeletedSubscribers = "0"
	}
	active, err := strconv.Atoi(item.ActiveSubscribers)
	if err != nil || active < 0 {
		return ListMetadata{}, fmt.Errorf("active subscriber count invalid")
	}
	nonDeleted, err := strconv.Atoi(item.NonDeletedSubscribers)
	if err != nil || nonDeleted < 0 {
		return ListMetadata{}, fmt.Errorf("non-deleted subscriber count invalid")
	}
	metadata.ActiveSubscribers, metadata.NonDeletedSubscribers = active, nonDeleted
	return metadata, nil
}
