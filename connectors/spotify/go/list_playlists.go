// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spotify

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListUserPlaylistsInput selects one page from the authenticated user's playlists.
type ListUserPlaylistsInput struct {
	// Limit is 1 through 50; zero uses 20 playlists per page.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based starting playlist position.
	Offset int `json:"offset,omitempty"`
}

// SearchPlaylistsInput selects one Spotify playlist catalog search page.
type SearchPlaylistsInput struct {
	// Query is nonempty search text; do not place secrets in it.
	Query string `json:"query"`
	// Market is an optional two-letter ISO country code; blank uses the authorized user's country.
	Market string `json:"market,omitempty"`
	// Limit is 1 through 10 in current Spotify development mode; zero uses 10.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based starting playlist position.
	Offset int `json:"offset,omitempty"`
}

// PlaylistSummaryPage is one bounded page of playlist metadata.
type PlaylistSummaryPage struct {
	// Playlists are the available playlists in provider order.
	Playlists []PlaylistSummary `json:"playlists"`
	// Limit is the page size.
	Limit int `json:"limit"`
	// Offset is the zero-based starting position.
	Offset int `json:"offset"`
	// Total is Spotify's total matching playlist count.
	Total int `json:"total"`
	// NextOffset is the next page start, omitted at the end.
	NextOffset int `json:"nextOffset,omitempty"`
}

// ListUserPlaylistsOperation lists playlists for the authenticated Spotify user.
type ListUserPlaylistsOperation struct{ client *Client }

// ListUserPlaylists returns the Query bound to this client.
func (client *Client) ListUserPlaylists() ListUserPlaylistsOperation {
	return ListUserPlaylistsOperation{client: client}
}

// Definition returns the immutable connector operation definition.
func (ListUserPlaylistsOperation) Definition() sdkgo.QueryDefinition {
	return ListUserPlaylistsDefinition
}

// Invoke reads one user playlist page without exposing provider response text in failures.
func (operation ListUserPlaylistsOperation) Invoke(call sdkgo.Call, input ListUserPlaylistsInput) sdkgo.QueryAttempt[PlaylistSummaryPage] {
	limit, offset, ok := validatePlaylistPage(input.Limit, input.Offset)
	if !ok {
		return playlistPageFailure(ListUserPlaylistsBranchDefect, sdkgo.FailureValidation, "limit or offset is outside Spotify's range")
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	return operation.client.readPlaylistPage(call, "/me/playlists", query, false, playlistPageBranches{
		listed: ListUserPlaylistsBranchListed, authorizationRevoked: ListUserPlaylistsBranchAuthorizationRevoked,
		accessDenied: ListUserPlaylistsBranchAccessDenied, notFound: ListUserPlaylistsBranchNotFound,
		providerRejected: ListUserPlaylistsBranchProviderRejected, invalidResponse: ListUserPlaylistsBranchInvalidResponse,
		defect: ListUserPlaylistsBranchDefect,
	})
}

// SearchPlaylistsOperation searches Spotify's playlist catalog.
type SearchPlaylistsOperation struct{ client *Client }

// SearchPlaylists returns the Query bound to this client.
func (client *Client) SearchPlaylists() SearchPlaylistsOperation {
	return SearchPlaylistsOperation{client: client}
}

// Definition returns the immutable connector operation definition.
func (SearchPlaylistsOperation) Definition() sdkgo.QueryDefinition { return SearchPlaylistsDefinition }

// Invoke reads one search page without exposing provider response text in failures.
func (operation SearchPlaylistsOperation) Invoke(call sdkgo.Call, input SearchPlaylistsInput) sdkgo.QueryAttempt[PlaylistSummaryPage] {
	queryText := strings.TrimSpace(input.Query)
	market := strings.ToUpper(strings.TrimSpace(input.Market))
	requestedLimit := input.Limit
	if requestedLimit == 0 {
		requestedLimit = 10
	}
	limit, offset, ok := validatePlaylistPage(requestedLimit, input.Offset)
	if !ok || queryText == "" || len(queryText) > 256 || !utf8.ValidString(queryText) ||
		limit > 10 ||
		strings.IndexFunc(queryText, unicode.IsControl) >= 0 ||
		(market != "" && !isASCIICountryCode(market)) {
		return playlistPageFailure(SearchPlaylistsBranchDefect, sdkgo.FailureValidation, "query, market, limit, or offset is invalid")
	}
	query := url.Values{"q": {queryText}, "type": {"playlist"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if market != "" {
		query.Set("market", market)
	}
	return operation.client.readPlaylistPage(call, "/search", query, true, playlistPageBranches{
		listed: SearchPlaylistsBranchListed, authorizationRevoked: SearchPlaylistsBranchAuthorizationRevoked,
		accessDenied: SearchPlaylistsBranchAccessDenied, notFound: SearchPlaylistsBranchNotFound,
		providerRejected: SearchPlaylistsBranchProviderRejected, invalidResponse: SearchPlaylistsBranchInvalidResponse,
		defect: SearchPlaylistsBranchDefect,
	})
}

type playlistPageBranches struct {
	listed, authorizationRevoked, accessDenied, notFound, providerRejected, invalidResponse, defect sdkgo.BranchID
}

func (client *Client) readPlaylistPage(call sdkgo.Call, path string, query url.Values, isSearch bool, branches playlistPageBranches) sdkgo.QueryAttempt[PlaylistSummaryPage] {
	credential, failure := client.resolveCredential(call)
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, PlaylistSummaryPage{}, failure, sdkgo.Receipt{})
	}
	response, err := client.getSpotifyRead(call, &credential, path, query)
	if errors.Is(err, errResponseTooLarge) {
		return playlistPageFailure(branches.invalidResponse, sdkgo.FailureResponseTooLarge, "Spotify playlist page exceeds the configured size limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[PlaylistSummaryPage](spotifyFailure(sdkgo.FailureAvailability, "Spotify is unavailable"), 0)
	}
	switch response.statusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return playlistPageFailure(branches.authorizationRevoked, sdkgo.FailureAuthentication, "Spotify authorization is invalid or revoked")
	case http.StatusForbidden:
		return playlistPageFailure(branches.accessDenied, sdkgo.FailureAuthorization, "Spotify denied access to playlists")
	case http.StatusNotFound:
		return playlistPageFailure(branches.notFound, sdkgo.FailureNotFound, "Spotify playlist collection was not found")
	case http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[PlaylistSummaryPage](spotifyFailure(sdkgo.FailureRateLimit, "Spotify rate limit was reached"), retryDelay(response.header))
	default:
		if response.statusCode >= 500 {
			return sdkgo.NewQueryRetry[PlaylistSummaryPage](spotifyFailure(sdkgo.FailureAvailability, "Spotify is unavailable"), 0)
		}
		return playlistPageFailure(branches.providerRejected, sdkgo.FailureProviderRejection, "Spotify rejected the playlist page request")
	}
	page, err := decodePlaylistSummaryPage(response.body, isSearch)
	if err != nil {
		return playlistPageFailure(branches.invalidResponse, sdkgo.FailureProtocol, "Spotify returned invalid playlist page metadata")
	}
	return sdkgo.NewQueryBranch(branches.listed, page, nil, sdkgo.Receipt{})
}

func decodePlaylistSummaryPage(body []byte, isSearch bool) (PlaylistSummaryPage, error) {
	var providerPage struct {
		Limit  int                      `json:"limit"`
		Offset int                      `json:"offset"`
		Total  int                      `json:"total"`
		Items  []spotifyPlaylistSummary `json:"items"`
	}
	if isSearch {
		var wrapper struct {
			Playlists json.RawMessage `json:"playlists"`
		}
		if err := decodeJSON(body, &wrapper); err != nil || len(wrapper.Playlists) == 0 {
			return PlaylistSummaryPage{}, errors.New("missing playlist search page")
		}
		body = wrapper.Playlists
	}
	if err := decodeJSON(body, &providerPage); err != nil || providerPage.Limit < 1 || providerPage.Limit > 50 || providerPage.Offset < 0 || providerPage.Total < 0 || len(providerPage.Items) > providerPage.Limit {
		return PlaylistSummaryPage{}, errors.New("invalid Spotify playlist page")
	}
	page := PlaylistSummaryPage{Playlists: make([]PlaylistSummary, 0, len(providerPage.Items)), Limit: providerPage.Limit, Offset: providerPage.Offset, Total: providerPage.Total}
	for _, providerPlaylist := range providerPage.Items {
		playlist, err := convertPlaylistSummary(providerPlaylist)
		if err != nil {
			return PlaylistSummaryPage{}, err
		}
		page.Playlists = append(page.Playlists, playlist)
	}
	if providerPage.Offset+providerPage.Limit < providerPage.Total {
		page.NextOffset = providerPage.Offset + providerPage.Limit
	}
	return page, nil
}

func validatePlaylistPage(limit, offset int) (int, int, bool) {
	if limit == 0 {
		limit = 20
	}
	return limit, offset, limit >= 1 && limit <= 50 && offset >= 0 && offset <= 2147483647
}

func isASCIICountryCode(value string) bool {
	return len(value) == 2 && value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z'
}

func playlistPageFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PlaylistSummaryPage] {
	failure := spotifyFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PlaylistSummaryPage{}, &failure, sdkgo.Receipt{})
}
