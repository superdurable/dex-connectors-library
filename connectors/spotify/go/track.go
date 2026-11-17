// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spotify

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetTrackInput identifies one Spotify track and an optional market.
type GetTrackInput struct {
	// TrackID is the 22-character ID from a Spotify track URL.
	TrackID string `json:"trackId"`
	// Market is an optional two-letter ISO country code.
	Market string `json:"market,omitempty"`
}

// SearchTracksInput selects one Spotify track catalog search page.
type SearchTracksInput struct {
	// Query is nonempty search text, with no secrets.
	Query string `json:"query"`
	// Market is an optional two-letter ISO country code.
	Market string `json:"market,omitempty"`
	// Limit is 1 through 10 in current Spotify development mode; zero uses 10.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based page start.
	Offset int `json:"offset,omitempty"`
}

// TrackSummaryPage is one bounded page of track summaries.
type TrackSummaryPage struct {
	// Tracks are the matching tracks in provider order.
	Tracks []TrackSummary `json:"tracks"`
	// Limit is the page size returned by Spotify.
	Limit int `json:"limit"`
	// Offset is the zero-based page start.
	Offset int `json:"offset"`
	// Total is Spotify's matching track count.
	Total int `json:"total"`
	// NextOffset is the next page start, omitted at the end.
	NextOffset int `json:"nextOffset,omitempty"`
}

// GetTrackOperation reads one track.
type GetTrackOperation struct{ client *Client }

// GetTrack returns the Query bound to this client.
func (client *Client) GetTrack() GetTrackOperation { return GetTrackOperation{client: client} }

// Definition returns the immutable connector operation definition.
func (GetTrackOperation) Definition() sdkgo.QueryDefinition { return GetTrackDefinition }

// Invoke reads one track using the authorized Spotify connection.
func (operation GetTrackOperation) Invoke(call sdkgo.Call, input GetTrackInput) sdkgo.QueryAttempt[TrackSummary] {
	market, validMarket := validateCatalogMarket(input.Market)
	if !spotifyIDPattern.MatchString(input.TrackID) || !validMarket {
		return catalogDefect[TrackSummary]("getTrack", GetTrackBranchDefect)
	}
	query := url.Values{}
	if market != "" {
		query.Set("market", market)
	}
	return readCatalogQuery(operation.client, call, "getTrack", "/tracks/"+url.PathEscape(input.TrackID), query,
		catalogBranches{GetTrackBranchFound, GetTrackBranchAuthorizationRevoked, GetTrackBranchAccessDenied,
			GetTrackBranchNotFound, GetTrackBranchProviderRejected, GetTrackBranchInvalidResponse, GetTrackBranchDefect},
		func(body []byte) (TrackSummary, error) {
			var track spotifyTrack
			if err := decodeJSON(body, &track); err != nil || track.ID != input.TrackID {
				return TrackSummary{}, errors.New("invalid track")
			}
			return convertTrackSummary(track)
		})
}

// SearchTracksOperation searches Spotify tracks.
type SearchTracksOperation struct{ client *Client }

// SearchTracks returns the Query bound to this client.
func (client *Client) SearchTracks() SearchTracksOperation {
	return SearchTracksOperation{client: client}
}

// Definition returns the immutable connector operation definition.
func (SearchTracksOperation) Definition() sdkgo.QueryDefinition { return SearchTracksDefinition }

// Invoke reads one track-search page using the authorized Spotify connection.
func (operation SearchTracksOperation) Invoke(call sdkgo.Call, input SearchTracksInput) sdkgo.QueryAttempt[TrackSummaryPage] {
	queryText := strings.TrimSpace(input.Query)
	market, validMarket := validateCatalogMarket(input.Market)
	requestedLimit := input.Limit
	if requestedLimit == 0 {
		requestedLimit = 10
	}
	limit, offset, validPage := validatePlaylistPage(requestedLimit, input.Offset)
	if !validCatalogQuery(queryText) || !validMarket || !validPage || limit > 10 {
		return catalogDefect[TrackSummaryPage]("searchTracks", SearchTracksBranchDefect)
	}
	query := url.Values{"q": {queryText}, "type": {"track"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if market != "" {
		query.Set("market", market)
	}
	return readCatalogQuery(operation.client, call, "searchTracks", "/search", query,
		catalogBranches{SearchTracksBranchListed, SearchTracksBranchAuthorizationRevoked, SearchTracksBranchAccessDenied,
			SearchTracksBranchNotFound, SearchTracksBranchProviderRejected, SearchTracksBranchInvalidResponse, SearchTracksBranchDefect},
		decodeTrackSearchPage)
}

func decodeTrackSearchPage(body []byte) (TrackSummaryPage, error) {
	var wrapper struct {
		Tracks json.RawMessage `json:"tracks"`
	}
	if err := decodeJSON(body, &wrapper); err != nil || len(wrapper.Tracks) == 0 {
		return TrackSummaryPage{}, errors.New("missing track search page")
	}
	var providerPage struct {
		Limit  int            `json:"limit"`
		Offset int            `json:"offset"`
		Total  int            `json:"total"`
		Items  []spotifyTrack `json:"items"`
	}
	if err := decodeJSON(wrapper.Tracks, &providerPage); err != nil || providerPage.Limit < 1 || providerPage.Limit > 10 || providerPage.Offset < 0 || providerPage.Total < 0 || len(providerPage.Items) > providerPage.Limit {
		return TrackSummaryPage{}, errors.New("invalid track search page")
	}
	page := TrackSummaryPage{Tracks: make([]TrackSummary, 0, len(providerPage.Items)), Limit: providerPage.Limit, Offset: providerPage.Offset, Total: providerPage.Total}
	for _, item := range providerPage.Items {
		track, err := convertTrackSummary(item)
		if err != nil {
			return TrackSummaryPage{}, err
		}
		page.Tracks = append(page.Tracks, track)
	}
	if page.Offset+page.Limit < page.Total {
		page.NextOffset = page.Offset + page.Limit
	}
	return page, nil
}
