// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webflow

import (
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListLiveCollectionItemsOperation reads published CMS items.
type ListLiveCollectionItemsOperation struct{ client *Client }

// ListLiveCollectionItems returns this client's typed operation.
func (client *Client) ListLiveCollectionItems() ListLiveCollectionItemsOperation {
	return ListLiveCollectionItemsOperation{client}
}

// Definition returns the generated operation contract.
func (ListLiveCollectionItemsOperation) Definition() sdkgo.QueryDefinition {
	return ListLiveCollectionItemsDefinition
}

// Invoke lists a page of published CMS items.
func (operation ListLiveCollectionItemsOperation) Invoke(call sdkgo.Call, input ListCollectionItemsInput) sdkgo.QueryAttempt[ListCollectionItemsOutput] {
	path, query, _, err := encodeListCollectionItemsInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListLiveCollectionItemsBranchDefect, ListCollectionItemsOutput{}, failure("listLiveCollectionItems", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "listLiveCollectionItems", http.MethodGet, path+"/live", query, nil, false, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[ListCollectionItemsOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, ListCollectionItemsOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "items") {
		return sdkgo.NewQueryBranch(ListLiveCollectionItemsBranchInvalidResponse, ListCollectionItemsOutput{}, failure("listLiveCollectionItems", sdkgo.FailureProtocol, "provider returned an incomplete item list"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(ListLiveCollectionItemsBranchListed, ListCollectionItemsOutput{Data: json.RawMessage(response.body)}, nil, sdkgo.Receipt{})
}

// GetLiveCollectionItemOperation reads a published CMS item.
type GetLiveCollectionItemOperation struct{ client *Client }

// GetLiveCollectionItem returns this client's typed operation.
func (client *Client) GetLiveCollectionItem() GetLiveCollectionItemOperation {
	return GetLiveCollectionItemOperation{client}
}

// Definition returns the generated operation contract.
func (GetLiveCollectionItemOperation) Definition() sdkgo.QueryDefinition {
	return GetLiveCollectionItemDefinition
}

// Invoke gets a published CMS item.
func (operation GetLiveCollectionItemOperation) Invoke(call sdkgo.Call, input GetCollectionItemInput) sdkgo.QueryAttempt[GetCollectionItemOutput] {
	path, _, _, err := encodeGetCollectionItemInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetLiveCollectionItemBranchDefect, GetCollectionItemOutput{}, failure("getLiveCollectionItem", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "getLiveCollectionItem", http.MethodGet, path+"/live", nil, nil, true, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[GetCollectionItemOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, GetCollectionItemOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "id") {
		return sdkgo.NewQueryBranch(GetLiveCollectionItemBranchInvalidResponse, GetCollectionItemOutput{}, failure("getLiveCollectionItem", sdkgo.FailureProtocol, "provider returned an incomplete item"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetLiveCollectionItemBranchFound, GetCollectionItemOutput{Data: json.RawMessage(response.body)}, nil, sdkgo.Receipt{})
}
