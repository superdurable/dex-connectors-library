// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package square

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CatalogObject contains the complete Square catalog object for round-trip replacement.
type CatalogObject struct {
	// Object preserves Square object fields, including child variations, for a full-replacement upsert.
	Object map[string]any `json:"object"`
}

// CatalogPage is one page of Square catalog items.
type CatalogPage struct {
	// Objects contains complete provider catalog objects for the page.
	Objects []map[string]any `json:"objects"`
	// Cursor continues listing or searching when set.
	Cursor string `json:"cursor"`
}

// ListCatalogObjectsInput selects one catalog item page.
type ListCatalogObjectsInput struct {
	// Cursor continues a previous page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// SearchCatalogObjectsInput searches item names and descriptions by one keyword.
type SearchCatalogObjectsInput struct {
	// Keyword is one nonblank text search term.
	Keyword string `json:"keyword"`
	// Cursor continues a previous search page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// GetCatalogObjectInput identifies one Square catalog object.
type GetCatalogObjectInput struct {
	// ObjectID is the Square catalog object ID.
	ObjectID string `json:"objectId"`
}

// UpsertCatalogObjectInput supplies a complete Square ITEM object for creation or replacement.
type UpsertCatalogObjectInput struct {
	// Object is the complete provider CatalogObject, including all fields and child variations to keep.
	Object map[string]any `json:"object"`
}

// DeleteCatalogObjectInput identifies one Square catalog object to delete.
type DeleteCatalogObjectInput struct {
	// ObjectID is the Square catalog object ID.
	ObjectID string `json:"objectId"`
}

// DeletedCatalogObjects reports every object ID removed by a cascading delete.
type DeletedCatalogObjects struct {
	// DeletedObjectIDs includes the requested object and any dependent variations.
	DeletedObjectIDs []string `json:"deletedObjectIds"`
}

// ListCatalogObjectsOperation lists Square catalog items.
type ListCatalogObjectsOperation struct{ client *Client }

// SearchCatalogObjectsOperation searches Square catalog items.
type SearchCatalogObjectsOperation struct{ client *Client }

// GetCatalogObjectOperation reads a Square catalog object.
type GetCatalogObjectOperation struct{ client *Client }

// UpsertCatalogObjectOperation creates or replaces a Square catalog item.
type UpsertCatalogObjectOperation struct{ client *Client }

// DeleteCatalogObjectOperation deletes a Square catalog item.
type DeleteCatalogObjectOperation struct{ client *Client }

// ListCatalogObjects binds the listCatalogObjects Query to this client.
func (client *Client) ListCatalogObjects() ListCatalogObjectsOperation {
	return ListCatalogObjectsOperation{client}
}

// SearchCatalogObjects binds the searchCatalogObjects Query to this client.
func (client *Client) SearchCatalogObjects() SearchCatalogObjectsOperation {
	return SearchCatalogObjectsOperation{client}
}

// GetCatalogObject binds the getCatalogObject Query to this client.
func (client *Client) GetCatalogObject() GetCatalogObjectOperation {
	return GetCatalogObjectOperation{client}
}

// UpsertCatalogObject binds the upsertCatalogObject Mutation to this client.
func (client *Client) UpsertCatalogObject() UpsertCatalogObjectOperation {
	return UpsertCatalogObjectOperation{client}
}

// DeleteCatalogObject binds the deleteCatalogObject Mutation to this client.
func (client *Client) DeleteCatalogObject() DeleteCatalogObjectOperation {
	return DeleteCatalogObjectOperation{client}
}

// Definition returns the manifest-generated contract.
func (ListCatalogObjectsOperation) Definition() sdkgo.QueryDefinition {
	return ListCatalogObjectsDefinition
}

// Definition returns the manifest-generated contract.
func (SearchCatalogObjectsOperation) Definition() sdkgo.QueryDefinition {
	return SearchCatalogObjectsDefinition
}

// Definition returns the manifest-generated contract.
func (GetCatalogObjectOperation) Definition() sdkgo.QueryDefinition {
	return GetCatalogObjectDefinition
}

// Definition returns the manifest-generated contract.
func (UpsertCatalogObjectOperation) Definition() sdkgo.MutationDefinition {
	return UpsertCatalogObjectDefinition
}

// Definition returns the manifest-generated contract.
func (DeleteCatalogObjectOperation) Definition() sdkgo.MutationDefinition {
	return DeleteCatalogObjectDefinition
}

// IdempotencyKey derives a stable Square-compatible key for full object replacement.
func (UpsertCatalogObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertCatalogObjectInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// IdempotencyKey identifies repeated deletion of one catalog object.
func (DeleteCatalogObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCatalogObjectInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// Invoke lists one page of ITEM catalog objects.
func (operation ListCatalogObjectsOperation) Invoke(call sdkgo.Call, input ListCatalogObjectsInput) sdkgo.QueryAttempt[CatalogPage] {
	query := url.Values{"types": {"ITEM"}}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	return executeRead(operation.client, call, "listCatalogObjects", "/catalog/list", query, ListCatalogObjectsBranchListed, ListCatalogObjectsBranchProviderRejected, ListCatalogObjectsBranchProviderRejected, ListCatalogObjectsBranchInvalidResponse, ListCatalogObjectsBranchDefect, decodeCatalogPage)
}

// Invoke reads one complete Square catalog object by ID.
func (operation GetCatalogObjectOperation) Invoke(call sdkgo.Call, input GetCatalogObjectInput) sdkgo.QueryAttempt[CatalogObject] {
	if !validPathSegment(input.ObjectID) {
		return failedAttempt[CatalogObject]("getCatalogObject", GetCatalogObjectBranchDefect, sdkgo.FailureValidation, "objectId must be a path segment")
	}
	return executeRead(operation.client, call, "getCatalogObject", "/catalog/object/"+url.PathEscape(input.ObjectID), url.Values{}, GetCatalogObjectBranchFound, GetCatalogObjectBranchNotFound, GetCatalogObjectBranchProviderRejected, GetCatalogObjectBranchInvalidResponse, GetCatalogObjectBranchDefect, decodeCatalogObject)
}

func decodeCatalogPage(body []byte) (CatalogPage, error) {
	var page CatalogPage
	if err := json.Unmarshal(body, &page); err != nil {
		return CatalogPage{}, err
	}
	if page.Objects == nil {
		page.Objects = []map[string]any{}
	}
	return page, nil
}

func decodeCatalogObject(body []byte) (CatalogObject, error) {
	var result CatalogObject
	if err := json.Unmarshal(body, &result); err != nil {
		return CatalogObject{}, err
	}
	if result.Object == nil || result.Object["id"] == nil {
		return CatalogObject{}, errors.New("missing catalog object")
	}
	return result, nil
}

// Invoke searches ITEM catalog objects by one text keyword.
func (operation SearchCatalogObjectsOperation) Invoke(call sdkgo.Call, input SearchCatalogObjectsInput) sdkgo.QueryAttempt[CatalogPage] {
	if input.Keyword == "" {
		return failedAttempt[CatalogPage]("searchCatalogObjects", SearchCatalogObjectsBranchDefect, sdkgo.FailureValidation, "keyword is required")
	}
	body := map[string]any{"object_types": []string{"ITEM"}, "query": map[string]any{"text_query": map[string]any{"keywords": []string{input.Keyword}}}}
	if input.Cursor != "" {
		body["cursor"] = input.Cursor
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return failedAttempt[CatalogPage]("searchCatalogObjects", SearchCatalogObjectsBranchDefect, sdkgo.FailureLocalDefect, "catalog search input is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/catalog/search", bytes.NewReader(payload))
	if err != nil {
		return failedAttempt[CatalogPage]("searchCatalogObjects", SearchCatalogObjectsBranchDefect, sdkgo.FailureLocalDefect, "catalog search request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := operation.client.authorize(call, "searchCatalogObjects", request, SearchCatalogObjectsBranchDefect, SearchCatalogObjectsBranchProviderRejected, SearchCatalogObjectsBranchInvalidResponse)
	if authorization.failure != nil {
		return sdkgo.NewQueryBranch(authorization.branch, CatalogPage{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[CatalogPage](*failureFor("searchCatalogObjects", sdkgo.FailureAvailability, "provider catalog search failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[CatalogPage](*failureFor("searchCatalogObjects", sdkgo.FailureRateLimit, "provider rate limited catalog search"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewQueryRetry[CatalogPage](*failureFor("searchCatalogObjects", sdkgo.FailureAvailability, "provider catalog search is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return failedAttempt[CatalogPage]("searchCatalogObjects", SearchCatalogObjectsBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected catalog search with HTTP "+strconv.Itoa(response.StatusCode))
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedAttempt[CatalogPage]("searchCatalogObjects", SearchCatalogObjectsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[CatalogPage](*failureFor("searchCatalogObjects", sdkgo.FailureAvailability, "provider catalog search response was interrupted"), 0)
	}
	page, err := decodeCatalogPage(responseBody)
	if err != nil {
		return failedAttempt[CatalogPage]("searchCatalogObjects", SearchCatalogObjectsBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned invalid JSON")
	}
	return sdkgo.NewQueryBranch(SearchCatalogObjectsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke creates or fully replaces a Square ITEM object with a stable provider key.
func (operation UpsertCatalogObjectOperation) Invoke(call sdkgo.Call, input UpsertCatalogObjectInput) sdkgo.MutationAttempt[CatalogObject] {
	if input.Object == nil || input.Object["type"] != "ITEM" {
		return catalogUpsertDefect()
	}
	objectID, isString := input.Object["id"].(string)
	if !isString || objectID == "" {
		return catalogUpsertDefect()
	}
	itemData, isObject := input.Object["item_data"].(map[string]any)
	if !isObject || itemData["name"] == nil {
		return catalogUpsertDefect()
	}
	payload, err := json.Marshal(map[string]any{"idempotency_key": call.IdempotencyKey, "object": input.Object})
	if err != nil {
		return catalogUpsertDefect()
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/catalog/object", bytes.NewReader(payload))
	if err != nil {
		return catalogUpsertDefect()
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := operation.client.authorize(call, "upsertCatalogObject", request, UpsertCatalogObjectBranchDefect, UpsertCatalogObjectBranchProviderRejected, UpsertCatalogObjectBranchInvalidResponse)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, CatalogObject{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[CatalogObject](*failureFor("upsertCatalogObject", sdkgo.FailureAvailability, "provider catalog upsert outcome is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[CatalogObject](*failureFor("upsertCatalogObject", sdkgo.FailureRateLimit, "provider rate limited catalog upsert"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[CatalogObject](*failureFor("upsertCatalogObject", sdkgo.FailureAvailability, "provider catalog upsert is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return sdkgo.NewMutationBranch(UpsertCatalogObjectBranchProviderRejected, CatalogObject{}, failureFor("upsertCatalogObject", sdkgo.FailureProviderRejection, "provider rejected catalog upsert with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationBranch(UpsertCatalogObjectBranchInvalidResponse, CatalogObject{}, failureFor("upsertCatalogObject", sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit"), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[CatalogObject](*failureFor("upsertCatalogObject", sdkgo.FailureAvailability, "provider catalog upsert response was interrupted"), 0)
	}
	object, err := decodeCatalogObject(responseBody)
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertCatalogObjectBranchInvalidResponse, CatalogObject{}, failureFor("upsertCatalogObject", sdkgo.FailureProtocol, "provider returned invalid JSON"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(UpsertCatalogObjectBranchUpserted, object, nil, sdkgo.Receipt{})
}

func catalogUpsertDefect() sdkgo.MutationAttempt[CatalogObject] {
	return sdkgo.NewMutationBranch(UpsertCatalogObjectBranchDefect, CatalogObject{}, failureFor("upsertCatalogObject", sdkgo.FailureValidation, "complete ITEM catalog object is required"), sdkgo.Receipt{})
}

// Invoke deletes one catalog item and reports its dependent objects.
func (operation DeleteCatalogObjectOperation) Invoke(call sdkgo.Call, input DeleteCatalogObjectInput) sdkgo.MutationAttempt[DeletedCatalogObjects] {
	if !validPathSegment(input.ObjectID) {
		return sdkgo.NewMutationBranch(DeleteCatalogObjectBranchDefect, DeletedCatalogObjects{}, failureFor("deleteCatalogObject", sdkgo.FailureValidation, "objectId must be a path segment"), sdkgo.Receipt{})
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodDelete, operation.client.baseURL+"/catalog/object/"+url.PathEscape(input.ObjectID), nil)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteCatalogObjectBranchDefect, DeletedCatalogObjects{}, failureFor("deleteCatalogObject", sdkgo.FailureLocalDefect, "catalog delete request could not be built"), sdkgo.Receipt{})
	}
	request.Header.Set("Accept", "application/json")
	authorization := operation.client.authorize(call, "deleteCatalogObject", request, DeleteCatalogObjectBranchDefect, DeleteCatalogObjectBranchProviderRejected, DeleteCatalogObjectBranchInvalidResponse)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, DeletedCatalogObjects{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedCatalogObjects](*failureFor("deleteCatalogObject", sdkgo.FailureAvailability, "provider catalog delete outcome is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[DeletedCatalogObjects](*failureFor("deleteCatalogObject", sdkgo.FailureRateLimit, "provider rate limited catalog deletion"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[DeletedCatalogObjects](*failureFor("deleteCatalogObject", sdkgo.FailureAvailability, "provider catalog deletion is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound:
		return sdkgo.NewMutationBranch(DeleteCatalogObjectBranchNotFound, DeletedCatalogObjects{}, failureFor("deleteCatalogObject", sdkgo.FailureNotFound, "catalog object was not found"), sdkgo.Receipt{})
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return sdkgo.NewMutationBranch(DeleteCatalogObjectBranchProviderRejected, DeletedCatalogObjects{}, failureFor("deleteCatalogObject", sdkgo.FailureProviderRejection, "provider rejected catalog deletion with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationBranch(DeleteCatalogObjectBranchInvalidResponse, DeletedCatalogObjects{}, failureFor("deleteCatalogObject", sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit"), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedCatalogObjects](*failureFor("deleteCatalogObject", sdkgo.FailureAvailability, "provider catalog deletion response was interrupted"), 0)
	}
	var result struct {
		DeletedObjectIDs []string `json:"deleted_object_ids"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil || len(result.DeletedObjectIDs) == 0 {
		return sdkgo.NewMutationBranch(DeleteCatalogObjectBranchInvalidResponse, DeletedCatalogObjects{}, failureFor("deleteCatalogObject", sdkgo.FailureProtocol, "provider returned invalid JSON"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(DeleteCatalogObjectBranchDeleted, DeletedCatalogObjects{DeletedObjectIDs: result.DeletedObjectIDs}, nil, sdkgo.Receipt{})
}
