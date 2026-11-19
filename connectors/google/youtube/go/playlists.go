// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetPlaylistInput identifies one playlist by the ID in its YouTube URL.
type GetPlaylistInput struct {
	// PlaylistID is the ID after list= in a YouTube playlist URL.
	PlaylistID string `json:"playlistId"`
}

// ListPlaylistsInput selects the connected account's or one channel's playlists.
type ListPlaylistsInput struct {
	// Mine selects playlists owned by the connected account.
	Mine bool `json:"mine,omitempty"`
	// ChannelID selects public playlists of this channel instead of Mine.
	ChannelID string `json:"channelId,omitempty"`
	// MaxResults bounds one page from 1 to 50; zero uses YouTube's default.
	MaxResults int `json:"maxResults,omitempty"`
	// PageToken is the opaque cursor returned by a previous page.
	PageToken string `json:"pageToken,omitempty"`
}

// Playlist contains the public-facing identity and metadata of one playlist.
type Playlist struct {
	// ID is the YouTube playlist ID.
	ID string `json:"id"`
	// Title is untrusted provider text.
	Title string `json:"title"`
	// Description is untrusted provider text.
	Description string `json:"description"`
	// PrivacyStatus is public, unlisted, or private.
	PrivacyStatus string `json:"privacyStatus"`
}

// PlaylistsPage contains one bounded page of playlists.
type PlaylistsPage struct {
	// Playlists are playlist identities and metadata.
	Playlists []Playlist `json:"playlists"`
	// NextPageToken is blank on the last page.
	NextPageToken string `json:"nextPageToken"`
}

// UpdatePlaylistInput replaces one playlist's editable metadata.
type UpdatePlaylistInput struct {
	// PlaylistID is the playlist to replace.
	PlaylistID string `json:"playlistId"`
	// Title is the complete replacement title.
	Title string `json:"title"`
	// Description is the complete replacement description; blank clears it.
	Description string `json:"description"`
	// PrivacyStatus is public, unlisted, or private.
	PrivacyStatus string `json:"privacyStatus"`
}

// DeletedPlaylist identifies the playlist confirmed absent.
type DeletedPlaylist struct {
	// PlaylistID is the requested playlist ID.
	PlaylistID string `json:"playlistId"`
}

type providerPlaylist struct {
	ID      string `json:"id"`
	Snippet struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"snippet"`
	Status struct {
		PrivacyStatus string `json:"privacyStatus"`
	} `json:"status"`
}

type providerPlaylistPage struct {
	Items         []providerPlaylist `json:"items"`
	NextPageToken string             `json:"nextPageToken"`
}

// GetPlaylistOperation reads one playlist.
type GetPlaylistOperation struct{ client *Client }

// ListPlaylistsOperation reads one page of playlists.
type ListPlaylistsOperation struct{ client *Client }

// UpdatePlaylistOperation replaces a playlist's editable metadata.
type UpdatePlaylistOperation struct{ client *Client }

// DeletePlaylistOperation removes a playlist by ID.
type DeletePlaylistOperation struct{ client *Client }

// GetPlaylist binds the playlist lookup Query.
func (client *Client) GetPlaylist() GetPlaylistOperation { return GetPlaylistOperation{client} }

// ListPlaylists binds the playlist list Query.
func (client *Client) ListPlaylists() ListPlaylistsOperation { return ListPlaylistsOperation{client} }

// UpdatePlaylist binds the playlist replacement Mutation.
func (client *Client) UpdatePlaylist() UpdatePlaylistOperation {
	return UpdatePlaylistOperation{client}
}

// DeletePlaylist binds the playlist delete Mutation.
func (client *Client) DeletePlaylist() DeletePlaylistOperation {
	return DeletePlaylistOperation{client}
}

var playlistBranches = operationBranches{
	notFound: GetPlaylistBranchNotFound, quotaExhausted: GetPlaylistBranchQuotaExhausted,
	rejected: GetPlaylistBranchProviderRejected, invalidResponse: GetPlaylistBranchInvalidResponse,
	defect: GetPlaylistBranchDefect,
}
var listPlaylistsBranches = operationBranches{
	notFound: ListPlaylistsBranchNotFound, quotaExhausted: ListPlaylistsBranchQuotaExhausted,
	rejected: ListPlaylistsBranchProviderRejected, invalidResponse: ListPlaylistsBranchInvalidResponse,
	defect: ListPlaylistsBranchDefect,
}
var updatePlaylistBranches = operationBranches{
	notFound: UpdatePlaylistBranchNotFound, quotaExhausted: UpdatePlaylistBranchQuotaExhausted,
	rejected: UpdatePlaylistBranchProviderRejected, invalidResponse: UpdatePlaylistBranchInvalidResponse,
	defect: UpdatePlaylistBranchDefect,
}
var deletePlaylistBranches = operationBranches{
	notFound: DeletePlaylistBranchNotFound, quotaExhausted: DeletePlaylistBranchQuotaExhausted,
	rejected: DeletePlaylistBranchProviderRejected, invalidResponse: DeletePlaylistBranchInvalidResponse,
	defect: DeletePlaylistBranchDefect,
}

// Definition returns the generated playlist lookup contract.
func (GetPlaylistOperation) Definition() sdkgo.QueryDefinition { return GetPlaylistDefinition }

// Definition returns the generated playlist list contract.
func (ListPlaylistsOperation) Definition() sdkgo.QueryDefinition { return ListPlaylistsDefinition }

// Definition returns the generated playlist replacement contract.
func (UpdatePlaylistOperation) Definition() sdkgo.MutationDefinition { return UpdatePlaylistDefinition }

// Definition returns the generated playlist delete contract.
func (DeletePlaylistOperation) Definition() sdkgo.MutationDefinition { return DeletePlaylistDefinition }

// IdempotencyKey uses the stable Step call ID for this absolute replacement.
func (UpdatePlaylistOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePlaylistInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey uses the stable Step call ID for deletion.
func (DeletePlaylistOperation) IdempotencyKey(callID sdkgo.CallID, _ GetPlaylistInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one playlist, selecting notFound for an empty list.
func (operation GetPlaylistOperation) Invoke(call sdkgo.Call, input GetPlaylistInput) sdkgo.QueryAttempt[Playlist] {
	if !playlistIDPattern.MatchString(input.PlaylistID) {
		return sdkgo.NewQueryBranch(GetPlaylistBranchDefect, Playlist{}, providerFailurePointer("getPlaylist", sdkgo.FailureValidation, "playlistId is invalid"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "getPlaylist", providerRequest{method: http.MethodGet, path: "/playlists", query: url.Values{"part": {"snippet,status"}, "id": {input.PlaylistID}}}, input.PlaylistID)
	if outcome != nil {
		return queryAttemptForOutcome[Playlist](*outcome, playlistBranches)
	}
	receipt := operation.client.receipt(call, input.PlaylistID, response.header)
	var page providerPlaylistPage
	if decodeResponse(response.body, &page) != nil || len(page.Items) > 1 {
		return sdkgo.NewQueryBranch(GetPlaylistBranchInvalidResponse, Playlist{}, providerFailurePointer("getPlaylist", sdkgo.FailureProtocol, "YouTube returned an invalid playlist list"), receipt)
	}
	if len(page.Items) == 0 {
		return sdkgo.NewQueryBranch(GetPlaylistBranchNotFound, Playlist{}, providerFailurePointer("getPlaylist", sdkgo.FailureNotFound, "playlist is not visible"), receipt)
	}
	playlist, ok := convertProviderPlaylist(page.Items[0])
	if !ok || playlist.ID != input.PlaylistID {
		return sdkgo.NewQueryBranch(GetPlaylistBranchInvalidResponse, Playlist{}, providerFailurePointer("getPlaylist", sdkgo.FailureProtocol, "YouTube returned a different playlist"), receipt)
	}
	return sdkgo.NewQueryBranch(GetPlaylistBranchFound, playlist, nil, receipt)
}

// Invoke reads one page of playlists using one owner selector.
func (operation ListPlaylistsOperation) Invoke(call sdkgo.Call, input ListPlaylistsInput) sdkgo.QueryAttempt[PlaylistsPage] {
	query := url.Values{"part": {"snippet,status"}}
	if input.Mine == (input.ChannelID != "") || (input.ChannelID != "" && !channelIDPattern.MatchString(input.ChannelID)) || validatePageRequest(query, input.MaxResults, input.PageToken) != nil {
		return sdkgo.NewQueryBranch(ListPlaylistsBranchDefect, PlaylistsPage{}, providerFailurePointer("listPlaylists", sdkgo.FailureValidation, "select mine or one valid channelId and a valid page"), sdkgo.Receipt{})
	}
	if input.Mine {
		query.Set("mine", "true")
	} else {
		query.Set("channelId", input.ChannelID)
	}
	response, outcome := operation.client.exchange(call, "listPlaylists", providerRequest{method: http.MethodGet, path: "/playlists", query: query}, input.ChannelID)
	if outcome != nil {
		return queryAttemptForOutcome[PlaylistsPage](*outcome, listPlaylistsBranches)
	}
	receipt := operation.client.receipt(call, input.ChannelID, response.header)
	var page providerPlaylistPage
	if decodeResponse(response.body, &page) != nil || len(page.Items) > MaximumPageSize || validateReturnedPageToken(page.NextPageToken) != nil {
		return sdkgo.NewQueryBranch(ListPlaylistsBranchInvalidResponse, PlaylistsPage{}, providerFailurePointer("listPlaylists", sdkgo.FailureProtocol, "YouTube returned an invalid playlist page"), receipt)
	}
	result := PlaylistsPage{Playlists: make([]Playlist, 0, len(page.Items)), NextPageToken: page.NextPageToken}
	for _, item := range page.Items {
		playlist, ok := convertProviderPlaylist(item)
		if !ok {
			return sdkgo.NewQueryBranch(ListPlaylistsBranchInvalidResponse, PlaylistsPage{}, providerFailurePointer("listPlaylists", sdkgo.FailureProtocol, "YouTube returned an incomplete playlist"), receipt)
		}
		result.Playlists = append(result.Playlists, playlist)
	}
	return sdkgo.NewQueryBranch(ListPlaylistsBranchListed, result, nil, receipt)
}

// Invoke replaces a playlist's editable snippet and privacy status.
func (operation UpdatePlaylistOperation) Invoke(call sdkgo.Call, input UpdatePlaylistInput) sdkgo.MutationAttempt[Playlist] {
	if !playlistIDPattern.MatchString(input.PlaylistID) || strings.TrimSpace(input.Title) == "" || !validPlaylistPrivacy(input.PrivacyStatus) {
		return sdkgo.NewMutationBranch(UpdatePlaylistBranchDefect, Playlist{}, providerFailurePointer("updatePlaylist", sdkgo.FailureValidation, "playlist ID, title, or privacy status is invalid"), sdkgo.Receipt{})
	}
	body, err := json.Marshal(map[string]any{"id": input.PlaylistID, "snippet": map[string]string{"title": input.Title, "description": input.Description}, "status": map[string]string{"privacyStatus": input.PrivacyStatus}})
	if err != nil {
		return sdkgo.NewMutationBranch(UpdatePlaylistBranchDefect, Playlist{}, providerFailurePointer("updatePlaylist", sdkgo.FailureLocalDefect, "playlist replacement could not be encoded"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "updatePlaylist", providerRequest{method: http.MethodPut, path: "/playlists", query: url.Values{"part": {"snippet,status"}}, body: body, requiresManage: true}, input.PlaylistID)
	if outcome != nil {
		return mutationAttemptForOutcome[Playlist](*outcome, updatePlaylistBranches)
	}
	receipt := operation.client.receipt(call, input.PlaylistID, response.header)
	var item providerPlaylist
	if decodeResponse(response.body, &item) != nil {
		return sdkgo.NewMutationBranch(UpdatePlaylistBranchInvalidResponse, Playlist{}, providerFailurePointer("updatePlaylist", sdkgo.FailureProtocol, "YouTube returned an invalid playlist response"), receipt)
	}
	playlist, ok := convertProviderPlaylist(item)
	if !ok || playlist.ID != input.PlaylistID || playlist.Title != input.Title || playlist.Description != input.Description || playlist.PrivacyStatus != input.PrivacyStatus {
		return sdkgo.NewMutationBranch(UpdatePlaylistBranchInvalidResponse, Playlist{}, providerFailurePointer("updatePlaylist", sdkgo.FailureProtocol, "YouTube returned different playlist values"), receipt)
	}
	return sdkgo.NewMutationBranch(UpdatePlaylistBranchUpdated, playlist, nil, receipt)
}

// Invoke deletes one playlist; 204 confirms that no response body is needed.
func (operation DeletePlaylistOperation) Invoke(call sdkgo.Call, input GetPlaylistInput) sdkgo.MutationAttempt[DeletedPlaylist] {
	if !playlistIDPattern.MatchString(input.PlaylistID) {
		return sdkgo.NewMutationBranch(DeletePlaylistBranchDefect, DeletedPlaylist{}, providerFailurePointer("deletePlaylist", sdkgo.FailureValidation, "playlistId is invalid"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "deletePlaylist", providerRequest{method: http.MethodDelete, path: "/playlists", query: url.Values{"id": {input.PlaylistID}}, requiresManage: true}, input.PlaylistID)
	if outcome != nil {
		return mutationAttemptForOutcome[DeletedPlaylist](*outcome, deletePlaylistBranches)
	}
	receipt := operation.client.receipt(call, input.PlaylistID, response.header)
	if response.statusCode != http.StatusNoContent {
		return sdkgo.NewMutationBranch(DeletePlaylistBranchInvalidResponse, DeletedPlaylist{}, providerFailurePointer("deletePlaylist", sdkgo.FailureProtocol, "YouTube returned an unexpected delete status"), receipt)
	}
	return sdkgo.NewMutationBranch(DeletePlaylistBranchDeleted, DeletedPlaylist{PlaylistID: input.PlaylistID}, nil, receipt)
}

func convertProviderPlaylist(item providerPlaylist) (Playlist, bool) {
	if !playlistIDPattern.MatchString(item.ID) || item.Snippet.Title == "" || !validPlaylistPrivacy(item.Status.PrivacyStatus) {
		return Playlist{}, false
	}
	return Playlist{ID: item.ID, Title: item.Snippet.Title, Description: item.Snippet.Description, PrivacyStatus: item.Status.PrivacyStatus}, true
}

func validPlaylistPrivacy(value string) bool {
	return value == "public" || value == "unlisted" || value == "private"
}
