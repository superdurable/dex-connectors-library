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

// GetArtistInput identifies one Spotify artist.
type GetArtistInput struct {
	// ArtistID is the 22-character ID from a Spotify artist URL.
	ArtistID string `json:"artistId"`
}

// ArtistSummary contains bounded artist metadata and a Spotify attribution link.
type ArtistSummary struct {
	// ID is the stable Spotify artist identifier.
	ID string `json:"id"`
	// Name is the artist's display name.
	Name string `json:"name"`
	// SpotifyURL links to the artist on Spotify.
	SpotifyURL string `json:"spotifyUrl,omitempty"`
	// URI is the Spotify artist URI.
	URI string `json:"uri,omitempty"`
	// Genres are Spotify's genre labels, when present.
	Genres []string `json:"genres"`
}

// ListArtistAlbumsInput selects one page of albums associated with an artist.
type ListArtistAlbumsInput struct {
	// ArtistID is the 22-character Spotify artist identifier.
	ArtistID string `json:"artistId"`
	// Market is an optional two-letter ISO country code.
	Market string `json:"market,omitempty"`
	// IncludeGroups optionally filters album, single, appears_on, and compilation groups.
	IncludeGroups []string `json:"includeGroups,omitempty"`
	// Limit is 1 through 50; zero uses 20 albums.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based page start.
	Offset int `json:"offset,omitempty"`
}

// SearchArtistsInput selects one Spotify artist catalog search page.
type SearchArtistsInput struct {
	// Query is nonempty search text, with no secrets.
	Query string `json:"query"`
	// Market is an optional two-letter ISO country code.
	Market string `json:"market,omitempty"`
	// Limit is 1 through 10 in current Spotify development mode; zero uses 10.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based page start.
	Offset int `json:"offset,omitempty"`
}

// ArtistSummaryPage is one bounded page of artist summaries.
type ArtistSummaryPage struct {
	// Artists are the matching artists in provider order.
	Artists []ArtistSummary `json:"artists"`
	// Limit is the page size returned by Spotify.
	Limit int `json:"limit"`
	// Offset is the zero-based page start.
	Offset int `json:"offset"`
	// Total is Spotify's matching artist count.
	Total int `json:"total"`
	// NextOffset is the next page start, omitted at the end.
	NextOffset int `json:"nextOffset,omitempty"`
}

// GetArtistOperation reads one artist.
type GetArtistOperation struct{ client *Client }

// GetArtist returns the Query bound to this client.
func (client *Client) GetArtist() GetArtistOperation { return GetArtistOperation{client: client} }

// Definition returns the immutable connector operation definition.
func (GetArtistOperation) Definition() sdkgo.QueryDefinition { return GetArtistDefinition }

// Invoke reads one artist using the authorized Spotify connection.
func (operation GetArtistOperation) Invoke(call sdkgo.Call, input GetArtistInput) sdkgo.QueryAttempt[ArtistSummary] {
	if !spotifyIDPattern.MatchString(input.ArtistID) {
		return catalogDefect[ArtistSummary]("getArtist", GetArtistBranchDefect)
	}
	return readCatalogQuery(operation.client, call, "getArtist", "/artists/"+url.PathEscape(input.ArtistID), nil,
		catalogBranches{GetArtistBranchFound, GetArtistBranchAuthorizationRevoked, GetArtistBranchAccessDenied,
			GetArtistBranchNotFound, GetArtistBranchProviderRejected, GetArtistBranchInvalidResponse, GetArtistBranchDefect},
		func(body []byte) (ArtistSummary, error) {
			var artist spotifyArtist
			if err := decodeJSON(body, &artist); err != nil || artist.ID != input.ArtistID {
				return ArtistSummary{}, errors.New("invalid artist")
			}
			return convertArtistSummary(artist)
		})
}

// ListArtistAlbumsOperation reads one page of an artist's albums.
type ListArtistAlbumsOperation struct{ client *Client }

// ListArtistAlbums returns the Query bound to this client.
func (client *Client) ListArtistAlbums() ListArtistAlbumsOperation {
	return ListArtistAlbumsOperation{client: client}
}

// Definition returns the immutable connector operation definition.
func (ListArtistAlbumsOperation) Definition() sdkgo.QueryDefinition {
	return ListArtistAlbumsDefinition
}

// Invoke reads one artist-album page using the authorized Spotify connection.
func (operation ListArtistAlbumsOperation) Invoke(call sdkgo.Call, input ListArtistAlbumsInput) sdkgo.QueryAttempt[AlbumSummaryPage] {
	market, validMarket := validateCatalogMarket(input.Market)
	limit, offset, validPage := validatePlaylistPage(input.Limit, input.Offset)
	groups, validGroups := validateArtistAlbumGroups(input.IncludeGroups)
	if !spotifyIDPattern.MatchString(input.ArtistID) || !validMarket || !validPage || !validGroups {
		return catalogDefect[AlbumSummaryPage]("listArtistAlbums", ListArtistAlbumsBranchDefect)
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if market != "" {
		query.Set("market", market)
	}
	if len(groups) > 0 {
		query.Set("include_groups", strings.Join(groups, ","))
	}
	return readCatalogQuery(operation.client, call, "listArtistAlbums", "/artists/"+url.PathEscape(input.ArtistID)+"/albums", query,
		catalogBranches{ListArtistAlbumsBranchListed, ListArtistAlbumsBranchAuthorizationRevoked, ListArtistAlbumsBranchAccessDenied,
			ListArtistAlbumsBranchNotFound, ListArtistAlbumsBranchProviderRejected, ListArtistAlbumsBranchInvalidResponse, ListArtistAlbumsBranchDefect},
		func(body []byte) (AlbumSummaryPage, error) { return decodeAlbumPage(body, 50) })
}

// SearchArtistsOperation searches Spotify artists.
type SearchArtistsOperation struct{ client *Client }

// SearchArtists returns the Query bound to this client.
func (client *Client) SearchArtists() SearchArtistsOperation {
	return SearchArtistsOperation{client: client}
}

// Definition returns the immutable connector operation definition.
func (SearchArtistsOperation) Definition() sdkgo.QueryDefinition { return SearchArtistsDefinition }

// Invoke reads one artist-search page using the authorized Spotify connection.
func (operation SearchArtistsOperation) Invoke(call sdkgo.Call, input SearchArtistsInput) sdkgo.QueryAttempt[ArtistSummaryPage] {
	queryText := strings.TrimSpace(input.Query)
	market, validMarket := validateCatalogMarket(input.Market)
	requestedLimit := input.Limit
	if requestedLimit == 0 {
		requestedLimit = 10
	}
	limit, offset, validPage := validatePlaylistPage(requestedLimit, input.Offset)
	if !validCatalogQuery(queryText) || !validMarket || !validPage || limit > 10 {
		return catalogDefect[ArtistSummaryPage]("searchArtists", SearchArtistsBranchDefect)
	}
	query := url.Values{"q": {queryText}, "type": {"artist"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if market != "" {
		query.Set("market", market)
	}
	return readCatalogQuery(operation.client, call, "searchArtists", "/search", query,
		catalogBranches{SearchArtistsBranchListed, SearchArtistsBranchAuthorizationRevoked, SearchArtistsBranchAccessDenied,
			SearchArtistsBranchNotFound, SearchArtistsBranchProviderRejected, SearchArtistsBranchInvalidResponse, SearchArtistsBranchDefect},
		decodeArtistSearchPage)
}

func validateArtistAlbumGroups(groups []string) ([]string, bool) {
	if len(groups) > 4 {
		return nil, false
	}
	seen := make(map[string]bool, len(groups))
	for _, group := range groups {
		switch group {
		case "album", "single", "appears_on", "compilation":
		default:
			return nil, false
		}
		if seen[group] {
			return nil, false
		}
		seen[group] = true
	}
	return groups, true
}

func convertArtistSummary(providerArtist spotifyArtist) (ArtistSummary, error) {
	if !spotifyIDPattern.MatchString(providerArtist.ID) || validateMetadataString(providerArtist.Name, "artist name", true) != nil {
		return ArtistSummary{}, errors.New("invalid Spotify artist")
	}
	spotifyLink, err := spotifyURL(providerArtist.ExternalURLs.Spotify)
	if err != nil {
		return ArtistSummary{}, err
	}
	artistURI, err := spotifyURI(providerArtist.URI)
	if err != nil {
		return ArtistSummary{}, err
	}
	genres := make([]string, 0, len(providerArtist.Genres))
	for _, genre := range providerArtist.Genres {
		if validateMetadataString(genre, "artist genre", true) != nil {
			return ArtistSummary{}, errors.New("invalid Spotify artist genre")
		}
		genres = append(genres, genre)
	}
	return ArtistSummary{ID: providerArtist.ID, Name: providerArtist.Name, SpotifyURL: spotifyLink, URI: artistURI, Genres: genres}, nil
}

func decodeArtistSearchPage(body []byte) (ArtistSummaryPage, error) {
	var wrapper struct {
		Artists json.RawMessage `json:"artists"`
	}
	if err := decodeJSON(body, &wrapper); err != nil || len(wrapper.Artists) == 0 {
		return ArtistSummaryPage{}, errors.New("missing artist search page")
	}
	var providerPage struct {
		Limit  int             `json:"limit"`
		Offset int             `json:"offset"`
		Total  int             `json:"total"`
		Items  []spotifyArtist `json:"items"`
	}
	if err := decodeJSON(wrapper.Artists, &providerPage); err != nil || providerPage.Limit < 1 || providerPage.Limit > 10 || providerPage.Offset < 0 || providerPage.Total < 0 || len(providerPage.Items) > providerPage.Limit {
		return ArtistSummaryPage{}, errors.New("invalid artist search page")
	}
	page := ArtistSummaryPage{Artists: make([]ArtistSummary, 0, len(providerPage.Items)), Limit: providerPage.Limit, Offset: providerPage.Offset, Total: providerPage.Total}
	for _, item := range providerPage.Items {
		artist, err := convertArtistSummary(item)
		if err != nil {
			return ArtistSummaryPage{}, err
		}
		page.Artists = append(page.Artists, artist)
	}
	if page.Offset+page.Limit < page.Total {
		page.NextOffset = page.Offset + page.Limit
	}
	return page, nil
}
