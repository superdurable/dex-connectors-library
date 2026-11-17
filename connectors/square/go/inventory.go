// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package square

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// InventoryCount is Square's computed quantity for a catalog variation at a location.
type InventoryCount struct {
	// CatalogObjectID identifies the catalog variation.
	CatalogObjectID string `json:"catalog_object_id"`
	// LocationID identifies the seller location.
	LocationID string `json:"location_id"`
	// State is the inventory state, such as IN_STOCK.
	State string `json:"state"`
	// Quantity is Square's decimal quantity string.
	Quantity string `json:"quantity"`
}

// InventoryCounts is one page of Square inventory counts.
type InventoryCounts struct {
	// Counts contains the computed inventory quantities.
	Counts []InventoryCount `json:"counts"`
	// Cursor continues a paginated inventory read when set.
	Cursor string `json:"cursor"`
}

// RetrieveInventoryCountInput identifies one catalog variation and optional location.
type RetrieveInventoryCountInput struct {
	// CatalogObjectID is the Square ITEM_VARIATION ID.
	CatalogObjectID string `json:"catalogObjectId"`
	// LocationID restricts the read to one location; blank includes all locations.
	LocationID string `json:"locationId,omitempty"`
	// Cursor continues a previous inventory page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// SetInventoryPhysicalCountInput reconciles one observed physical quantity.
type SetInventoryPhysicalCountInput struct {
	// CatalogObjectID is the Square ITEM_VARIATION ID.
	CatalogObjectID string `json:"catalogObjectId"`
	// LocationID identifies the location where the stock was counted.
	LocationID string `json:"locationId"`
	// Quantity is a nonnegative decimal quantity string.
	Quantity string `json:"quantity"`
	// OccurredAt is the observation time in RFC3339 format, retained across retries.
	OccurredAt string `json:"occurredAt"`
}

// RetrieveInventoryCountOperation reads a catalog variation's computed stock.
type RetrieveInventoryCountOperation struct{ client *Client }

// SetInventoryPhysicalCountOperation reconciles a catalog variation's stock.
type SetInventoryPhysicalCountOperation struct{ client *Client }

var inventoryDecimalQuantity = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// RetrieveInventoryCount binds the retrieveInventoryCount Query to this client.
func (client *Client) RetrieveInventoryCount() RetrieveInventoryCountOperation {
	return RetrieveInventoryCountOperation{client}
}

// SetInventoryPhysicalCount binds the setInventoryPhysicalCount Mutation to this client.
func (client *Client) SetInventoryPhysicalCount() SetInventoryPhysicalCountOperation {
	return SetInventoryPhysicalCountOperation{client}
}

// Definition returns the manifest-generated contract.
func (RetrieveInventoryCountOperation) Definition() sdkgo.QueryDefinition {
	return RetrieveInventoryCountDefinition
}

// Definition returns the manifest-generated contract.
func (SetInventoryPhysicalCountOperation) Definition() sdkgo.MutationDefinition {
	return SetInventoryPhysicalCountDefinition
}

// IdempotencyKey uses the Dex call UUID required by Square's inventory endpoint.
func (SetInventoryPhysicalCountOperation) IdempotencyKey(callID sdkgo.CallID, _ SetInventoryPhysicalCountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the computed counts for one catalog variation.
func (operation RetrieveInventoryCountOperation) Invoke(call sdkgo.Call, input RetrieveInventoryCountInput) sdkgo.QueryAttempt[InventoryCounts] {
	if !validPathSegment(input.CatalogObjectID) || (input.LocationID != "" && !validPathSegment(input.LocationID)) {
		return failedAttempt[InventoryCounts]("retrieveInventoryCount", RetrieveInventoryCountBranchDefect, sdkgo.FailureValidation, "valid catalogObjectId and locationId are required")
	}
	query := url.Values{}
	if input.LocationID != "" {
		query.Set("location_ids", input.LocationID)
	}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	return executeRead(operation.client, call, "retrieveInventoryCount", "/inventory/"+url.PathEscape(input.CatalogObjectID), query, RetrieveInventoryCountBranchFound, RetrieveInventoryCountBranchNotFound, RetrieveInventoryCountBranchProviderRejected, RetrieveInventoryCountBranchInvalidResponse, RetrieveInventoryCountBranchDefect, decodeInventoryCounts)
}

func decodeInventoryCounts(body []byte) (InventoryCounts, error) {
	var result InventoryCounts
	if err := json.Unmarshal(body, &result); err != nil {
		return InventoryCounts{}, err
	}
	if result.Counts == nil {
		result.Counts = []InventoryCount{}
	}
	return result, nil
}

// Invoke sends one physical count with a stable UUID key and observation time.
func (operation SetInventoryPhysicalCountOperation) Invoke(call sdkgo.Call, input SetInventoryPhysicalCountInput) sdkgo.MutationAttempt[InventoryCounts] {
	_, timeError := time.Parse(time.RFC3339, input.OccurredAt)
	if !validPathSegment(input.CatalogObjectID) || !validPathSegment(input.LocationID) || !inventoryDecimalQuantity.MatchString(input.Quantity) || timeError != nil {
		return inventoryMutationDefect()
	}
	payload, err := json.Marshal(map[string]any{
		"idempotency_key": call.IdempotencyKey,
		"changes": []any{map[string]any{"type": "PHYSICAL_COUNT", "physical_count": map[string]any{
			"catalog_object_id": input.CatalogObjectID, "location_id": input.LocationID,
			"state": "IN_STOCK", "quantity": input.Quantity, "occurred_at": input.OccurredAt,
		}}},
	})
	if err != nil {
		return inventoryMutationDefect()
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/inventory/changes/batch-create", bytes.NewReader(payload))
	if err != nil {
		return inventoryMutationDefect()
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := operation.client.authorize(call, "setInventoryPhysicalCount", request, SetInventoryPhysicalCountBranchDefect, SetInventoryPhysicalCountBranchProviderRejected, SetInventoryPhysicalCountBranchInvalidResponse)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, InventoryCounts{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[InventoryCounts](*failureFor("setInventoryPhysicalCount", sdkgo.FailureAvailability, "provider inventory outcome is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[InventoryCounts](*failureFor("setInventoryPhysicalCount", sdkgo.FailureRateLimit, "provider rate limited inventory change"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[InventoryCounts](*failureFor("setInventoryPhysicalCount", sdkgo.FailureAvailability, "provider inventory change is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return sdkgo.NewMutationBranch(SetInventoryPhysicalCountBranchProviderRejected, InventoryCounts{}, failureFor("setInventoryPhysicalCount", sdkgo.FailureProviderRejection, "provider rejected inventory change with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationBranch(SetInventoryPhysicalCountBranchInvalidResponse, InventoryCounts{}, failureFor("setInventoryPhysicalCount", sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit"), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[InventoryCounts](*failureFor("setInventoryPhysicalCount", sdkgo.FailureAvailability, "provider inventory response was interrupted"), 0)
	}
	counts, err := decodeInventoryCounts(responseBody)
	if err != nil || len(counts.Counts) == 0 {
		return sdkgo.NewMutationBranch(SetInventoryPhysicalCountBranchInvalidResponse, InventoryCounts{}, failureFor("setInventoryPhysicalCount", sdkgo.FailureProtocol, "provider returned invalid inventory JSON"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(SetInventoryPhysicalCountBranchReconciled, counts, nil, sdkgo.Receipt{})
}

func inventoryMutationDefect() sdkgo.MutationAttempt[InventoryCounts] {
	return sdkgo.NewMutationBranch(SetInventoryPhysicalCountBranchDefect, InventoryCounts{}, failureFor("setInventoryPhysicalCount", sdkgo.FailureValidation, "variation, location, nonnegative quantity, and RFC3339 occurredAt are required"), sdkgo.Receipt{})
}
