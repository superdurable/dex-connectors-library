// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListPlaylistItemsInput selects one page of a playlist in playlist order.
type ListPlaylistItemsInput struct {
	// PlaylistID is the playlist ID from youtube.com/playlist?list=<ID>, or a channel's UploadsPlaylistID.
	PlaylistID string `json:"playlistId"`
	// VideoID returns only items for this 11-character video ID; blank returns every item.
	VideoID string `json:"videoId,omitempty"`
	// MaxResults is the page size, 1 to 50; zero uses YouTube's default of 5.
	MaxResults int `json:"maxResults,omitempty"`
	// PageToken continues from the previous page's NextPageToken; blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// PlaylistItemPage is one page of playlist items.
type PlaylistItemPage struct {
	// Items holds this page in playlist position order, possibly empty.
	Items []PlaylistItem `json:"items"`
	// NextPageToken continues the listing; blank means the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
	// TotalResults is the number of items in the playlist, or matching VideoID.
	TotalResults int64 `json:"totalResults"`
}

// PlaylistItem is one entry of a playlist. Title is untrusted provider text.
type PlaylistItem struct {
	// ID is the playlist item ID, distinct from the video ID.
	ID string `json:"id"`
	// PlaylistID is the playlist holding the item.
	PlaylistID string `json:"playlistId"`
	// VideoID is the 11-character ID of the video.
	VideoID string `json:"videoId"`
	// Title is the video title, or "Private video" or "Deleted video" for unavailable videos.
	Title string `json:"title"`
	// Position is the zero-based position in the playlist.
	Position int64 `json:"position"`
	// AddedAt is when the item was added to the playlist, in UTC.
	AddedAt time.Time `json:"addedAt"`
	// VideoPublishedAt is when the video was published, or nil when it is private or deleted.
	VideoPublishedAt *time.Time `json:"videoPublishedAt,omitempty"`
}

type providerPlaylistItemList struct {
	NextPageToken string `json:"nextPageToken"`
	PageInfo      *struct {
		TotalResults int64 `json:"totalResults"`
	} `json:"pageInfo"`
	Items *[]providerPlaylistItem `json:"items"`
}

type providerPlaylistItem struct {
	ID      string `json:"id"`
	Snippet struct {
		PlaylistID  string `json:"playlistId"`
		Title       string `json:"title"`
		Position    int64  `json:"position"`
		PublishedAt string `json:"publishedAt"`
		ResourceID  struct {
			Kind    string `json:"kind"`
			VideoID string `json:"videoId"`
		} `json:"resourceId"`
	} `json:"snippet"`
	ContentDetails struct {
		VideoPublishedAt string `json:"videoPublishedAt"`
	} `json:"contentDetails"`
}

// ListPlaylistItemsOperation implements the listPlaylistItems connector Query. Each call costs one quota unit.
type ListPlaylistItemsOperation struct{ client *Client }

var listPlaylistItemsBranches = operationBranches{
	notFound: ListPlaylistItemsBranchNotFound, quotaExhausted: ListPlaylistItemsBranchQuotaExhausted, rejected: ListPlaylistItemsBranchProviderRejected,
	invalidResponse: ListPlaylistItemsBranchInvalidResponse, defect: ListPlaylistItemsBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (ListPlaylistItemsOperation) Definition() sdkgo.QueryDefinition {
	return ListPlaylistItemsDefinition
}

// Invoke sends one playlistItems.list read and classifies its attempt.
func (operation ListPlaylistItemsOperation) Invoke(call sdkgo.Call, input ListPlaylistItemsInput) sdkgo.QueryAttempt[PlaylistItemPage] {
	query, err := encodeListPlaylistItemsQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListPlaylistItemsBranchDefect, PlaylistItemPage{}, providerFailurePointer("listPlaylistItems", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	page, receipt, outcome := operation.client.listPlaylistItems(call, "listPlaylistItems", input, query, false)
	if outcome != nil {
		return queryAttemptForOutcome[PlaylistItemPage](*outcome, listPlaylistItemsBranches)
	}
	return sdkgo.NewQueryBranch(ListPlaylistItemsBranchListed, page, nil, receipt)
}

func encodeListPlaylistItemsQuery(input ListPlaylistItemsInput) (url.Values, error) {
	if !playlistIDPattern.MatchString(input.PlaylistID) {
		return nil, errors.New("playlistId must be a YouTube playlist ID")
	}
	query := url.Values{"part": {"snippet,contentDetails"}, "playlistId": {input.PlaylistID}}
	if input.VideoID != "" {
		if !videoIDPattern.MatchString(input.VideoID) {
			return nil, errors.New("videoId must be an 11-character YouTube video ID")
		}
		query.Set("videoId", input.VideoID)
	}
	if err := validatePageRequest(query, input.MaxResults, input.PageToken); err != nil {
		return nil, err
	}
	return query, nil
}

// listPlaylistItems reads and validates one page; addPlaylistItem uses it for its pre-insert check.
func (client *Client) listPlaylistItems(call sdkgo.Call, operation string, input ListPlaylistItemsInput, query url.Values, requiresManage bool) (PlaylistItemPage, sdkgo.Receipt, *providerOutcome) {
	response, outcome := client.exchange(call, operation, providerRequest{method: http.MethodGet, path: "/playlistItems", query: query, requiresManage: requiresManage}, input.PlaylistID)
	if outcome != nil {
		return PlaylistItemPage{}, outcome.receipt, outcome
	}
	receipt := client.receipt(call, input.PlaylistID, response.header)
	page, err := decodePlaylistItemPage(response.body, input)
	if err != nil {
		return PlaylistItemPage{}, receipt, &providerOutcome{disposition: outcomeInvalidResponse, failure: providerFailure(operation, sdkgo.FailureProtocol, err.Error()), receipt: receipt}
	}
	return page, receipt, nil
}

func decodePlaylistItemPage(body []byte, input ListPlaylistItemsInput) (PlaylistItemPage, error) {
	var decoded providerPlaylistItemList
	if err := decodeResponse(body, &decoded); err != nil {
		return PlaylistItemPage{}, err
	}
	if decoded.Items == nil || decoded.PageInfo == nil {
		return PlaylistItemPage{}, errors.New("YouTube returned an incomplete playlist page")
	}
	if len(*decoded.Items) > MaximumPageSize {
		return PlaylistItemPage{}, errors.New("YouTube returned more items than the page size")
	}
	if err := validateReturnedPageToken(decoded.NextPageToken); err != nil {
		return PlaylistItemPage{}, err
	}
	page := PlaylistItemPage{Items: make([]PlaylistItem, 0, len(*decoded.Items)), NextPageToken: decoded.NextPageToken, TotalResults: decoded.PageInfo.TotalResults}
	for _, item := range *decoded.Items {
		converted, err := convertProviderPlaylistItem(item, input.PlaylistID)
		if err != nil {
			return PlaylistItemPage{}, err
		}
		if input.VideoID != "" && converted.VideoID != input.VideoID {
			return PlaylistItemPage{}, errors.New("YouTube returned an item for a different video than the filter")
		}
		page.Items = append(page.Items, converted)
	}
	return page, nil
}

func convertProviderPlaylistItem(item providerPlaylistItem, playlistID string) (PlaylistItem, error) {
	if item.ID == "" || len(item.ID) > 128 || !pageTokenPattern.MatchString(item.ID) {
		return PlaylistItem{}, errors.New("YouTube returned a playlist item with an invalid ID")
	}
	if item.Snippet.PlaylistID != playlistID || item.Snippet.ResourceID.Kind != "youtube#video" || !videoIDPattern.MatchString(item.Snippet.ResourceID.VideoID) || item.Snippet.Position < 0 {
		return PlaylistItem{}, errors.New("YouTube returned a playlist item for another playlist or not for a valid video")
	}
	addedAt, err := parseOptionalTime(item.Snippet.PublishedAt)
	if err != nil || addedAt == nil {
		return PlaylistItem{}, errors.New("YouTube returned a playlist item without an added time")
	}
	videoPublishedAt, err := parseOptionalTime(item.ContentDetails.VideoPublishedAt)
	if err != nil {
		return PlaylistItem{}, err
	}
	return PlaylistItem{
		ID: item.ID, PlaylistID: item.Snippet.PlaylistID, VideoID: item.Snippet.ResourceID.VideoID, Title: item.Snippet.Title,
		Position: item.Snippet.Position, AddedAt: *addedAt, VideoPublishedAt: videoPublishedAt,
	}, nil
}

// AddPlaylistItemInput names the video to add to the end of a playlist the account manages.
type AddPlaylistItemInput struct {
	// PlaylistID is the playlist ID from youtube.com/playlist?list=<ID>; the authorized account must own it.
	PlaylistID string `json:"playlistId"`
	// VideoID is the 11-character ID of the video to add.
	VideoID string `json:"videoId"`
}

// AddedPlaylistItem is the playlist entry that holds the video.
type AddedPlaylistItem struct {
	// Item is the new entry, or the existing entry when WasAlreadyPresent is true.
	Item PlaylistItem `json:"item"`
	// WasAlreadyPresent reports that the playlist already held the video, so nothing was inserted.
	WasAlreadyPresent bool `json:"wasAlreadyPresent"`
}

// AddPlaylistItemOperation implements the addPlaylistItem connector Mutation.
// YouTube has no idempotency key for playlistItems.insert, so each attempt
// first lists the playlist filtered to the video and inserts only when it is
// absent. A retried attempt after an ambiguous insert therefore finds the item
// instead of adding it twice. Sync durability keeps attempts from overlapping.
// The check costs one quota unit and the insert 50. It requires a connection
// authorized with ManageAuthMethodID; a read-only connection selects
// providerRejected before any provider call.
type AddPlaylistItemOperation struct{ client *Client }

var addPlaylistItemBranches = operationBranches{
	notFound: AddPlaylistItemBranchNotFound, quotaExhausted: AddPlaylistItemBranchQuotaExhausted, rejected: AddPlaylistItemBranchProviderRejected,
	invalidResponse: AddPlaylistItemBranchInvalidResponse, defect: AddPlaylistItemBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (AddPlaylistItemOperation) Definition() sdkgo.MutationDefinition {
	return AddPlaylistItemDefinition
}

// IdempotencyKey derives the Dex key from the stable connector call ID; the provider-side guard is the presence check.
func (AddPlaylistItemOperation) IdempotencyKey(callID sdkgo.CallID, _ AddPlaylistItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke checks whether the playlist holds the video and inserts it only when absent.
func (operation AddPlaylistItemOperation) Invoke(call sdkgo.Call, input AddPlaylistItemInput) sdkgo.MutationAttempt[AddedPlaylistItem] {
	listInput := ListPlaylistItemsInput{PlaylistID: input.PlaylistID, VideoID: input.VideoID, MaxResults: 1}
	query, err := encodeListPlaylistItemsQuery(listInput)
	if err != nil || input.VideoID == "" {
		message := "videoId must be an 11-character YouTube video ID"
		if err != nil {
			message = err.Error()
		}
		return sdkgo.NewMutationBranch(AddPlaylistItemBranchDefect, AddedPlaylistItem{}, providerFailurePointer("addPlaylistItem", sdkgo.FailureValidation, message), sdkgo.Receipt{})
	}
	existing, receipt, outcome := operation.client.listPlaylistItems(call, "addPlaylistItem", listInput, query, true)
	if outcome != nil {
		return mutationAttemptForOutcome[AddedPlaylistItem](*outcome, addPlaylistItemBranches)
	}
	if len(existing.Items) > 0 {
		receipt.ProviderObjectID = existing.Items[0].ID
		return sdkgo.NewMutationBranch(AddPlaylistItemBranchAdded, AddedPlaylistItem{Item: existing.Items[0], WasAlreadyPresent: true}, nil, receipt)
	}
	body, err := json.Marshal(map[string]any{"snippet": map[string]any{
		"playlistId": input.PlaylistID, "resourceId": map[string]string{"kind": "youtube#video", "videoId": input.VideoID},
	}})
	if err != nil {
		return sdkgo.NewMutationBranch(AddPlaylistItemBranchDefect, AddedPlaylistItem{}, providerFailurePointer("addPlaylistItem", sdkgo.FailureLocalDefect, "the insert request could not be encoded"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "addPlaylistItem", providerRequest{
		method: http.MethodPost, path: "/playlistItems", query: url.Values{"part": {"snippet,contentDetails"}}, body: body, requiresManage: true,
	}, input.PlaylistID)
	if outcome != nil {
		if outcome.disposition == outcomeInvalidResponse {
			// The insert may have landed; the retried attempt's check finds it.
			return sdkgo.NewMutationRetry[AddedPlaylistItem](outcome.failure, 0)
		}
		return mutationAttemptForOutcome[AddedPlaylistItem](*outcome, addPlaylistItemBranches)
	}
	receipt = operation.client.receipt(call, input.PlaylistID, response.header)
	var inserted providerPlaylistItem
	if err := decodeResponse(response.body, &inserted); err != nil {
		return sdkgo.NewMutationRetry[AddedPlaylistItem](providerFailure("addPlaylistItem", sdkgo.FailureProtocol, "YouTube returned a malformed insert response; the retry checks the playlist"), 0)
	}
	item, err := convertProviderPlaylistItem(inserted, input.PlaylistID)
	if err != nil || item.VideoID != input.VideoID {
		return sdkgo.NewMutationBranch(AddPlaylistItemBranchInvalidResponse, AddedPlaylistItem{}, providerFailurePointer("addPlaylistItem", sdkgo.FailureProtocol, "YouTube returned an invalid or different inserted item"), receipt)
	}
	receipt.ProviderObjectID = item.ID
	return sdkgo.NewMutationBranch(AddPlaylistItemBranchAdded, AddedPlaylistItem{Item: item}, nil, receipt)
}
