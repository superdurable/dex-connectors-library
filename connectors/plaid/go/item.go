// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package plaid

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetItemInput reads the Item bound to this connection without additional input.
type GetItemInput struct{}

// ItemSummary identifies the Item and its enabled products without credentials.
type ItemSummary struct {
	// ItemID is the linked Plaid Item identity.
	ItemID string `json:"itemId"`
	// InstitutionID is the institution identity, when Plaid provides one.
	InstitutionID string `json:"institutionId,omitempty"`
	// Products are the products enabled on this Item.
	Products []string `json:"products"`
}

// GetItemOperation reads metadata for the Item bound to this connection.
type GetItemOperation struct{ client *Client }

// GetItem binds the Item Query to this client.
func (client *Client) GetItem() GetItemOperation { return GetItemOperation{client} }

// Definition returns the generated Item Query contract.
func (GetItemOperation) Definition() sdkgo.QueryDefinition { return GetItemDefinition }

// Invoke reads one Item's identity and enabled product names.
func (operation GetItemOperation) Invoke(call sdkgo.Call, _ GetItemInput) sdkgo.QueryAttempt[ItemSummary] {
	response := operation.client.postItem(call, "/item/get")
	if errors.Is(response.err, errCredential) {
		return itemBranch(GetItemBranchDefect, sdkgo.FailureAuthentication, "Plaid credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ItemSummary](itemFailure(sdkgo.FailureTransport, "Plaid request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ItemSummary](itemFailure(sdkgo.FailureRateLimit, "Plaid rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ItemSummary](itemFailure(sdkgo.FailureAvailability, "Plaid is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound || (response.err == nil && isPlaidItemNotFound(response.body)) {
		return itemBranch(GetItemBranchNotFound, sdkgo.FailureNotFound, "Plaid did not expose the Item")
	}
	if response.status != http.StatusOK {
		return itemBranch(GetItemBranchProviderRejected, sdkgo.FailureProviderRejection, "Plaid rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return itemBranch(GetItemBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Plaid response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ItemSummary](itemFailure(sdkgo.FailureTransport, "Plaid response was interrupted"), 0)
	}
	item, err := decodeItemSummary(response.body)
	if err != nil {
		return itemBranch(GetItemBranchInvalidResponse, sdkgo.FailureProtocol, "Plaid returned invalid Item JSON")
	}
	return sdkgo.NewQueryBranch(GetItemBranchFound, item, nil, sdkgo.Receipt{})
}

func decodeItemSummary(body []byte) (ItemSummary, error) {
	var wire struct {
		Item struct {
			ItemID        string   `json:"item_id"`
			InstitutionID string   `json:"institution_id"`
			Products      []string `json:"products"`
		} `json:"item"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ItemSummary{}, err
	}
	if !validItemID(wire.Item.ItemID) || wire.Item.Products == nil {
		return ItemSummary{}, errors.New("Plaid Item identity or products are missing")
	}
	for _, product := range wire.Item.Products {
		if product == "" {
			return ItemSummary{}, errors.New("Plaid Item product is invalid")
		}
	}
	return ItemSummary{ItemID: wire.Item.ItemID, InstitutionID: wire.Item.InstitutionID, Products: wire.Item.Products}, nil
}

func itemFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "plaid", Operation: "getItem", Message: message}
}

func itemBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ItemSummary] {
	failure := itemFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ItemSummary{}, &failure, sdkgo.Receipt{})
}

// RemoveItemInput removes the Item bound to this connection without additional input.
type RemoveItemInput struct{}

// ItemRemoval confirms that Plaid removed the linked Item or it was already absent.
type ItemRemoval struct {
	// Removed is true when the Item can no longer be accessed with this connection.
	Removed bool `json:"removed"`
}

// RemoveItemOperation disconnects the Item bound to this connection.
type RemoveItemOperation struct{ client *Client }

// RemoveItem binds the Item removal Mutation to this client.
func (client *Client) RemoveItem() RemoveItemOperation { return RemoveItemOperation{client} }

// Definition returns the generated Item removal contract.
func (RemoveItemOperation) Definition() sdkgo.MutationDefinition { return RemoveItemDefinition }

// IdempotencyKey binds repeated removal attempts to one Dex call identity.
func (RemoveItemOperation) IdempotencyKey(callID sdkgo.CallID, _ RemoveItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes the Item, treating ITEM_NOT_FOUND as a confirmed prior removal.
func (operation RemoveItemOperation) Invoke(call sdkgo.Call, _ RemoveItemInput) sdkgo.MutationAttempt[ItemRemoval] {
	response := operation.client.postItem(call, "/item/remove")
	if errors.Is(response.err, errCredential) {
		return removalBranch(RemoveItemBranchDefect, sdkgo.FailureAuthentication, "Plaid credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[ItemRemoval](removalFailure(sdkgo.FailureTransport, "Plaid request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ItemRemoval](removalFailure(sdkgo.FailureRateLimit, "Plaid rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ItemRemoval](removalFailure(sdkgo.FailureAvailability, "Plaid is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound || (response.err == nil && isPlaidItemNotFound(response.body)) {
		return sdkgo.NewMutationBranch(RemoveItemBranchRemoved, ItemRemoval{Removed: true}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return removalBranch(RemoveItemBranchProviderRejected, sdkgo.FailureProviderRejection, "Plaid rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[ItemRemoval](removalFailure(sdkgo.FailureTransport, "Plaid response was interrupted"), 0)
	}
	return sdkgo.NewMutationBranch(RemoveItemBranchRemoved, ItemRemoval{Removed: true}, nil, sdkgo.Receipt{})
}

func removalFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "plaid", Operation: "removeItem", Message: message}
}

func removalBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ItemRemoval] {
	failure := removalFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, ItemRemoval{}, &failure, sdkgo.Receipt{})
}
