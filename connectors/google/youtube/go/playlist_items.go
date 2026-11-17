// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube

import (
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// PlaylistItemIDInput selects one item by its playlist item ID.
type PlaylistItemIDInput struct {
	// PlaylistItemID is the ID returned by listPlaylistItems or addPlaylistItem.
	PlaylistItemID string `json:"playlistItemId"`
}

// DeletedPlaylistItem identifies an item confirmed deleted.
type DeletedPlaylistItem struct {
	// PlaylistItemID is the deleted playlist item ID.
	PlaylistItemID string `json:"playlistItemId"`
}

// GetPlaylistItemOperation reads one playlist item.
type GetPlaylistItemOperation struct{ client *Client }

// DeletePlaylistItemOperation deletes one playlist item.
type DeletePlaylistItemOperation struct{ client *Client }

// GetPlaylistItem binds the playlist item lookup Query.
func (client *Client) GetPlaylistItem() GetPlaylistItemOperation {
	return GetPlaylistItemOperation{client}
}

// DeletePlaylistItem binds the playlist item deletion Mutation.
func (client *Client) DeletePlaylistItem() DeletePlaylistItemOperation {
	return DeletePlaylistItemOperation{client}
}

var getPlaylistItemBranches = operationBranches{
	notFound: GetPlaylistItemBranchNotFound, quotaExhausted: GetPlaylistItemBranchQuotaExhausted,
	rejected: GetPlaylistItemBranchProviderRejected, invalidResponse: GetPlaylistItemBranchInvalidResponse,
	defect: GetPlaylistItemBranchDefect,
}
var deletePlaylistItemBranches = operationBranches{
	notFound: DeletePlaylistItemBranchNotFound, quotaExhausted: DeletePlaylistItemBranchQuotaExhausted,
	rejected: DeletePlaylistItemBranchProviderRejected, invalidResponse: DeletePlaylistItemBranchInvalidResponse,
	defect: DeletePlaylistItemBranchDefect,
}

// Definition returns the generated playlist item lookup contract.
func (GetPlaylistItemOperation) Definition() sdkgo.QueryDefinition { return GetPlaylistItemDefinition }

// Definition returns the generated playlist item deletion contract.
func (DeletePlaylistItemOperation) Definition() sdkgo.MutationDefinition {
	return DeletePlaylistItemDefinition
}

// IdempotencyKey uses the stable Step call ID for deletion.
func (DeletePlaylistItemOperation) IdempotencyKey(callID sdkgo.CallID, _ PlaylistItemIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one playlist item, preserving its provider ID and position.
func (operation GetPlaylistItemOperation) Invoke(call sdkgo.Call, input PlaylistItemIDInput) sdkgo.QueryAttempt[PlaylistItem] {
	if !playlistIDPattern.MatchString(input.PlaylistItemID) {
		return sdkgo.NewQueryBranch(GetPlaylistItemBranchDefect, PlaylistItem{}, providerFailurePointer("getPlaylistItem", sdkgo.FailureValidation, "playlistItemId is invalid"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "getPlaylistItem", providerRequest{method: http.MethodGet, path: "/playlistItems", query: url.Values{"part": {"snippet,contentDetails"}, "id": {input.PlaylistItemID}}}, input.PlaylistItemID)
	if outcome != nil {
		return queryAttemptForOutcome[PlaylistItem](*outcome, getPlaylistItemBranches)
	}
	receipt := operation.client.receipt(call, input.PlaylistItemID, response.header)
	var page providerPlaylistItemList
	if decodeResponse(response.body, &page) != nil || page.Items == nil || len(*page.Items) > 1 {
		return sdkgo.NewQueryBranch(GetPlaylistItemBranchInvalidResponse, PlaylistItem{}, providerFailurePointer("getPlaylistItem", sdkgo.FailureProtocol, "YouTube returned an invalid playlist item page"), receipt)
	}
	if len(*page.Items) == 0 {
		return sdkgo.NewQueryBranch(GetPlaylistItemBranchNotFound, PlaylistItem{}, providerFailurePointer("getPlaylistItem", sdkgo.FailureNotFound, "playlist item is not visible"), receipt)
	}
	item, err := convertProviderPlaylistItem((*page.Items)[0], (*page.Items)[0].Snippet.PlaylistID)
	if err != nil || item.ID != input.PlaylistItemID {
		return sdkgo.NewQueryBranch(GetPlaylistItemBranchInvalidResponse, PlaylistItem{}, providerFailurePointer("getPlaylistItem", sdkgo.FailureProtocol, "YouTube returned a different or invalid playlist item"), receipt)
	}
	return sdkgo.NewQueryBranch(GetPlaylistItemBranchFound, item, nil, receipt)
}

// Invoke removes one playlist item and accepts YouTube's empty 204 response.
func (operation DeletePlaylistItemOperation) Invoke(call sdkgo.Call, input PlaylistItemIDInput) sdkgo.MutationAttempt[DeletedPlaylistItem] {
	if !playlistIDPattern.MatchString(input.PlaylistItemID) {
		return sdkgo.NewMutationBranch(DeletePlaylistItemBranchDefect, DeletedPlaylistItem{}, providerFailurePointer("deletePlaylistItem", sdkgo.FailureValidation, "playlistItemId is invalid"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "deletePlaylistItem", providerRequest{method: http.MethodDelete, path: "/playlistItems", query: url.Values{"id": {input.PlaylistItemID}}, requiresManage: true}, input.PlaylistItemID)
	if outcome != nil {
		return mutationAttemptForOutcome[DeletedPlaylistItem](*outcome, deletePlaylistItemBranches)
	}
	receipt := operation.client.receipt(call, input.PlaylistItemID, response.header)
	if response.statusCode != http.StatusNoContent {
		return sdkgo.NewMutationBranch(DeletePlaylistItemBranchInvalidResponse, DeletedPlaylistItem{}, providerFailurePointer("deletePlaylistItem", sdkgo.FailureProtocol, "YouTube returned an unexpected delete status"), receipt)
	}
	return sdkgo.NewMutationBranch(DeletePlaylistItemBranchDeleted, DeletedPlaylistItem{PlaylistItemID: input.PlaylistItemID}, nil, receipt)
}
