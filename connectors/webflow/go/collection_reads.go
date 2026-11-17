// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webflow

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListCollectionsInput selects the site whose CMS collections are listed.
type ListCollectionsInput struct {
	// SiteID is the 24-character Webflow site ID.
	SiteID string `json:"siteId"`
}

// ListCollectionsOutput preserves Webflow's collection list.
type ListCollectionsOutput struct {
	// Data contains the collections array.
	Data json.RawMessage `json:"data"`
}

// GetCollectionInput selects a CMS collection.
type GetCollectionInput struct {
	// CollectionID is the 24-character CMS collection ID.
	CollectionID string `json:"collectionId"`
}

// GetCollectionOutput preserves the collection schema.
type GetCollectionOutput struct {
	// Data contains the collection and its fields.
	Data json.RawMessage `json:"data"`
}

// ListCollectionsOperation calls Webflow's site collection endpoint.
type ListCollectionsOperation struct{ client *Client }

// ListCollections returns this client's typed operation.
func (client *Client) ListCollections() ListCollectionsOperation {
	return ListCollectionsOperation{client}
}

// Definition returns the generated operation contract.
func (ListCollectionsOperation) Definition() sdkgo.QueryDefinition { return ListCollectionsDefinition }

// Invoke lists the site's CMS collections.
func (operation ListCollectionsOperation) Invoke(call sdkgo.Call, input ListCollectionsInput) sdkgo.QueryAttempt[ListCollectionsOutput] {
	if !objectID.MatchString(input.SiteID) {
		return sdkgo.NewQueryBranch(ListCollectionsBranchDefect, ListCollectionsOutput{}, failure("listCollections", sdkgo.FailureValidation, "site ID must be 24 hexadecimal characters"), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "listCollections", http.MethodGet, "/sites/"+url.PathEscape(input.SiteID)+"/collections", nil, nil, true, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[ListCollectionsOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, ListCollectionsOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "collections") {
		return sdkgo.NewQueryBranch(ListCollectionsBranchInvalidResponse, ListCollectionsOutput{}, failure("listCollections", sdkgo.FailureProtocol, "provider returned an incomplete collection list"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(ListCollectionsBranchListed, ListCollectionsOutput{Data: json.RawMessage(response.body)}, nil, sdkgo.Receipt{})
}

// GetCollectionOperation calls Webflow's collection detail endpoint.
type GetCollectionOperation struct{ client *Client }

// GetCollection returns this client's typed operation.
func (client *Client) GetCollection() GetCollectionOperation { return GetCollectionOperation{client} }

// Definition returns the generated operation contract.
func (GetCollectionOperation) Definition() sdkgo.QueryDefinition { return GetCollectionDefinition }

// Invoke gets a CMS collection and its field schema.
func (operation GetCollectionOperation) Invoke(call sdkgo.Call, input GetCollectionInput) sdkgo.QueryAttempt[GetCollectionOutput] {
	if !objectID.MatchString(input.CollectionID) {
		return sdkgo.NewQueryBranch(GetCollectionBranchDefect, GetCollectionOutput{}, failure("getCollection", sdkgo.FailureValidation, "collection ID must be 24 hexadecimal characters"), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "getCollection", http.MethodGet, "/collections/"+url.PathEscape(input.CollectionID), nil, nil, true, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[GetCollectionOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, GetCollectionOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "id") {
		return sdkgo.NewQueryBranch(GetCollectionBranchInvalidResponse, GetCollectionOutput{}, failure("getCollection", sdkgo.FailureProtocol, "provider returned an incomplete collection"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetCollectionBranchFound, GetCollectionOutput{Data: json.RawMessage(response.body)}, nil, sdkgo.Receipt{})
}
