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

// GetAlbumInput identifies one Spotify album and an optional market.
type GetAlbumInput struct {
	// AlbumID is the 22-character ID from a Spotify album URL.
	AlbumID string `json:"albumId"`
	// Market is an optional two-letter ISO country code; blank uses the authorized user's country.
	Market string `json:"market,omitempty"`
}

// AlbumSummary contains album metadata and Spotify attribution links.
type AlbumSummary struct {
	// ID is the stable Spotify album identifier.
	ID string `json:"id"`
	// Name is the album title.
	Name string `json:"name"`
	// SpotifyURL links to the album on Spotify.
	SpotifyURL string `json:"spotifyUrl,omitempty"`
	// URI is the Spotify album URI.
	URI string `json:"uri,omitempty"`
	// ReleaseDate is a year, year-month, or full date as reported by Spotify.
	ReleaseDate string `json:"releaseDate,omitempty"`
	// TotalTracks is the album's track count.
	TotalTracks int `json:"totalTracks"`
	// Artists are the album artists in provider order.
	Artists []PlaylistArtist `json:"artists"`
}

// ListAlbumTracksInput selects a bounded page of one album's tracks.
type ListAlbumTracksInput struct {
	// AlbumID is the 22-character Spotify album identifier.
	AlbumID string `json:"albumId"`
	// Market is an optional two-letter ISO country code.
	Market string `json:"market,omitempty"`
	// Limit is 1 through 50; zero uses 20 tracks.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based start of the page.
	Offset int `json:"offset,omitempty"`
}

// TrackSummary is bounded Spotify track metadata without audio or artwork.
type TrackSummary struct {
	// ID is the stable Spotify track identifier.
	ID string `json:"id"`
	// Name is the track title.
	Name string `json:"name"`
	// SpotifyURL links to the track on Spotify.
	SpotifyURL string `json:"spotifyUrl,omitempty"`
	// URI is the Spotify track URI.
	URI string `json:"uri,omitempty"`
	// DurationMilliseconds is the track length in milliseconds.
	DurationMilliseconds int `json:"durationMilliseconds"`
	// Artists are the track artists in provider order.
	Artists []PlaylistArtist `json:"artists"`
	// Album is present for full tracks and omitted for album-track listings.
	Album *AlbumSummary `json:"album,omitempty"`
}

// AlbumTrackPage is one bounded page of tracks from an album.
type AlbumTrackPage struct {
	// AlbumID identifies the requested album.
	AlbumID string `json:"albumId"`
	// Tracks are the available tracks in provider order.
	Tracks []TrackSummary `json:"tracks"`
	// Limit is the page size returned by Spotify.
	Limit int `json:"limit"`
	// Offset is the zero-based page start.
	Offset int `json:"offset"`
	// Total is the album's total track count.
	Total int `json:"total"`
	// NextOffset is the next page start, omitted at the end.
	NextOffset int `json:"nextOffset,omitempty"`
}

// SearchAlbumsInput selects one Spotify album catalog search page.
type SearchAlbumsInput struct {
	// Query is nonempty search text, with no secrets.
	Query string `json:"query"`
	// Market is an optional two-letter ISO country code.
	Market string `json:"market,omitempty"`
	// Limit is 1 through 10 in current Spotify development mode; zero uses 10.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based page start.
	Offset int `json:"offset,omitempty"`
}

// AlbumSummaryPage is one bounded page of album summaries.
type AlbumSummaryPage struct {
	// Albums are the matching albums in provider order.
	Albums []AlbumSummary `json:"albums"`
	// Limit is the page size returned by Spotify.
	Limit int `json:"limit"`
	// Offset is the zero-based page start.
	Offset int `json:"offset"`
	// Total is Spotify's matching album count.
	Total int `json:"total"`
	// NextOffset is the next page start, omitted at the end.
	NextOffset int `json:"nextOffset,omitempty"`
}

// GetAlbumOperation reads one album.
type GetAlbumOperation struct{ client *Client }

// GetAlbum returns the Query bound to this client.
func (client *Client) GetAlbum() GetAlbumOperation { return GetAlbumOperation{client: client} }

// Definition returns the immutable connector operation definition.
func (GetAlbumOperation) Definition() sdkgo.QueryDefinition { return GetAlbumDefinition }

// Invoke reads one album using the authorized Spotify connection.
func (operation GetAlbumOperation) Invoke(call sdkgo.Call, input GetAlbumInput) sdkgo.QueryAttempt[AlbumSummary] {
	market, ok := validateCatalogMarket(input.Market)
	if !spotifyIDPattern.MatchString(input.AlbumID) || !ok {
		return catalogDefect[AlbumSummary]("getAlbum", GetAlbumBranchDefect)
	}
	query := url.Values{}
	if market != "" {
		query.Set("market", market)
	}
	return readCatalogQuery(operation.client, call, "getAlbum", "/albums/"+url.PathEscape(input.AlbumID), query,
		catalogBranches{GetAlbumBranchFound, GetAlbumBranchAuthorizationRevoked, GetAlbumBranchAccessDenied,
			GetAlbumBranchNotFound, GetAlbumBranchProviderRejected, GetAlbumBranchInvalidResponse, GetAlbumBranchDefect},
		func(body []byte) (AlbumSummary, error) {
			var album spotifyAlbum
			if err := decodeJSON(body, &album); err != nil || album.ID != input.AlbumID {
				return AlbumSummary{}, errors.New("invalid album")
			}
			return convertAlbumSummary(album)
		})
}

// ListAlbumTracksOperation reads one bounded album-track page.
type ListAlbumTracksOperation struct{ client *Client }

// ListAlbumTracks returns the Query bound to this client.
func (client *Client) ListAlbumTracks() ListAlbumTracksOperation {
	return ListAlbumTracksOperation{client: client}
}

// Definition returns the immutable connector operation definition.
func (ListAlbumTracksOperation) Definition() sdkgo.QueryDefinition { return ListAlbumTracksDefinition }

// Invoke reads one album-track page using the authorized Spotify connection.
func (operation ListAlbumTracksOperation) Invoke(call sdkgo.Call, input ListAlbumTracksInput) sdkgo.QueryAttempt[AlbumTrackPage] {
	market, validMarket := validateCatalogMarket(input.Market)
	limit, offset, validPage := validatePlaylistPage(input.Limit, input.Offset)
	if !spotifyIDPattern.MatchString(input.AlbumID) || !validMarket || !validPage {
		return catalogDefect[AlbumTrackPage]("listAlbumTracks", ListAlbumTracksBranchDefect)
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if market != "" {
		query.Set("market", market)
	}
	return readCatalogQuery(operation.client, call, "listAlbumTracks", "/albums/"+url.PathEscape(input.AlbumID)+"/tracks", query,
		catalogBranches{ListAlbumTracksBranchListed, ListAlbumTracksBranchAuthorizationRevoked, ListAlbumTracksBranchAccessDenied,
			ListAlbumTracksBranchNotFound, ListAlbumTracksBranchProviderRejected, ListAlbumTracksBranchInvalidResponse, ListAlbumTracksBranchDefect},
		func(body []byte) (AlbumTrackPage, error) { return decodeAlbumTrackPage(body, input.AlbumID) })
}

// SearchAlbumsOperation searches Spotify albums.
type SearchAlbumsOperation struct{ client *Client }

// SearchAlbums returns the Query bound to this client.
func (client *Client) SearchAlbums() SearchAlbumsOperation {
	return SearchAlbumsOperation{client: client}
}

// Definition returns the immutable connector operation definition.
func (SearchAlbumsOperation) Definition() sdkgo.QueryDefinition { return SearchAlbumsDefinition }

// Invoke reads one album-search page using the authorized Spotify connection.
func (operation SearchAlbumsOperation) Invoke(call sdkgo.Call, input SearchAlbumsInput) sdkgo.QueryAttempt[AlbumSummaryPage] {
	queryText := strings.TrimSpace(input.Query)
	market, validMarket := validateCatalogMarket(input.Market)
	requestedLimit := input.Limit
	if requestedLimit == 0 {
		requestedLimit = 10
	}
	limit, offset, validPage := validatePlaylistPage(requestedLimit, input.Offset)
	if !validCatalogQuery(queryText) || !validMarket || !validPage || limit > 10 {
		return catalogDefect[AlbumSummaryPage]("searchAlbums", SearchAlbumsBranchDefect)
	}
	query := url.Values{"q": {queryText}, "type": {"album"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if market != "" {
		query.Set("market", market)
	}
	return readCatalogQuery(operation.client, call, "searchAlbums", "/search", query,
		catalogBranches{SearchAlbumsBranchListed, SearchAlbumsBranchAuthorizationRevoked, SearchAlbumsBranchAccessDenied,
			SearchAlbumsBranchNotFound, SearchAlbumsBranchProviderRejected, SearchAlbumsBranchInvalidResponse, SearchAlbumsBranchDefect},
		decodeAlbumSearchPage)
}

type catalogBranches struct {
	success, authorizationRevoked, accessDenied, notFound, providerRejected, invalidResponse, defect sdkgo.BranchID
}

func readCatalogQuery[T any](client *Client, call sdkgo.Call, operationID, path string, query url.Values, branches catalogBranches, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	credential, failure := client.resolveCredential(call)
	if failure != nil {
		corrected := spotifyOperationFailure(operationID, failure.Kind, failure.Message)
		return sdkgo.NewQueryBranch(branches.defect, *new(T), &corrected, sdkgo.Receipt{})
	}
	response, err := client.getSpotifyRead(call, &credential, path, query)
	if errors.Is(err, errResponseTooLarge) {
		return catalogFailure[T](operationID, branches.invalidResponse, sdkgo.FailureResponseTooLarge, "Spotify response exceeds the configured size limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](spotifyOperationFailure(operationID, sdkgo.FailureAvailability, "Spotify is unavailable"), 0)
	}
	switch response.statusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return catalogFailure[T](operationID, branches.authorizationRevoked, sdkgo.FailureAuthentication, "Spotify authorization is invalid or revoked")
	case http.StatusForbidden:
		return catalogFailure[T](operationID, branches.accessDenied, sdkgo.FailureAuthorization, "Spotify denied access to the resource")
	case http.StatusNotFound:
		return catalogFailure[T](operationID, branches.notFound, sdkgo.FailureNotFound, "Spotify resource was not found")
	case http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[T](spotifyOperationFailure(operationID, sdkgo.FailureRateLimit, "Spotify rate limit was reached"), retryDelay(response.header))
	default:
		if response.statusCode >= 500 {
			return sdkgo.NewQueryRetry[T](spotifyOperationFailure(operationID, sdkgo.FailureAvailability, "Spotify is unavailable"), 0)
		}
		return catalogFailure[T](operationID, branches.providerRejected, sdkgo.FailureProviderRejection, "Spotify rejected the resource query")
	}
	value, err := decode(response.body)
	if err != nil {
		return catalogFailure[T](operationID, branches.invalidResponse, sdkgo.FailureProtocol, "Spotify returned invalid resource metadata")
	}
	return sdkgo.NewQueryBranch(branches.success, value, nil, sdkgo.Receipt{})
}

func catalogDefect[T any](operationID string, branch sdkgo.BranchID) sdkgo.QueryAttempt[T] {
	return catalogFailure[T](operationID, branch, sdkgo.FailureValidation, "Spotify resource input is invalid")
}

func catalogFailure[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := spotifyOperationFailure(operationID, kind, message)
	return sdkgo.NewQueryBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func spotifyOperationFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "spotify", Operation: operationID, Message: message}
}

func validateCatalogMarket(value string) (string, bool) {
	market := strings.ToUpper(strings.TrimSpace(value))
	return market, market == "" || isASCIICountryCode(market)
}

func validCatalogQuery(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) == -1
}

func convertAlbumSummary(providerAlbum spotifyAlbum) (AlbumSummary, error) {
	album, err := convertAlbum(providerAlbum)
	if err != nil || !spotifyIDPattern.MatchString(album.ID) || providerAlbum.TotalTracks < 0 {
		return AlbumSummary{}, errors.New("invalid Spotify album")
	}
	return AlbumSummary{ID: album.ID, Name: album.Name, SpotifyURL: album.SpotifyURL, URI: album.URI,
		ReleaseDate: album.ReleaseDate, TotalTracks: providerAlbum.TotalTracks, Artists: album.Artists}, nil
}

func convertTrackSummary(providerTrack spotifyTrack) (TrackSummary, error) {
	if !spotifyIDPattern.MatchString(providerTrack.ID) || validateMetadataString(providerTrack.Name, "track name", true) != nil || providerTrack.Duration < 0 || providerTrack.Artists == nil {
		return TrackSummary{}, errors.New("invalid Spotify track")
	}
	spotifyLink, err := spotifyURL(providerTrack.ExternalURLs.Spotify)
	if err != nil {
		return TrackSummary{}, err
	}
	spotifyURI, err := spotifyURI(providerTrack.URI)
	if err != nil {
		return TrackSummary{}, err
	}
	artists, err := convertArtists(providerTrack.Artists)
	if err != nil {
		return TrackSummary{}, err
	}
	track := TrackSummary{ID: providerTrack.ID, Name: providerTrack.Name, SpotifyURL: spotifyLink, URI: spotifyURI,
		DurationMilliseconds: providerTrack.Duration, Artists: artists}
	if providerTrack.Album != nil {
		album, err := convertAlbumSummary(*providerTrack.Album)
		if err != nil {
			return TrackSummary{}, err
		}
		track.Album = &album
	}
	return track, nil
}

func decodeAlbumTrackPage(body []byte, albumID string) (AlbumTrackPage, error) {
	var providerPage struct {
		Limit  int            `json:"limit"`
		Offset int            `json:"offset"`
		Total  int            `json:"total"`
		Items  []spotifyTrack `json:"items"`
	}
	if err := decodeJSON(body, &providerPage); err != nil || providerPage.Limit < 1 || providerPage.Limit > 50 || providerPage.Offset < 0 || providerPage.Total < 0 || len(providerPage.Items) > providerPage.Limit {
		return AlbumTrackPage{}, errors.New("invalid Spotify album-track page")
	}
	page := AlbumTrackPage{AlbumID: albumID, Tracks: make([]TrackSummary, 0, len(providerPage.Items)), Limit: providerPage.Limit, Offset: providerPage.Offset, Total: providerPage.Total}
	for _, item := range providerPage.Items {
		track, err := convertTrackSummary(item)
		if err != nil {
			return AlbumTrackPage{}, err
		}
		page.Tracks = append(page.Tracks, track)
	}
	if page.Offset+page.Limit < page.Total {
		page.NextOffset = page.Offset + page.Limit
	}
	return page, nil
}

func decodeAlbumSearchPage(body []byte) (AlbumSummaryPage, error) {
	var wrapper struct {
		Albums json.RawMessage `json:"albums"`
	}
	if err := decodeJSON(body, &wrapper); err != nil || len(wrapper.Albums) == 0 {
		return AlbumSummaryPage{}, errors.New("missing album search page")
	}
	return decodeAlbumPage(wrapper.Albums, 10)
}

func decodeAlbumPage(body []byte, maximumLimit int) (AlbumSummaryPage, error) {
	var providerPage struct {
		Limit  int            `json:"limit"`
		Offset int            `json:"offset"`
		Total  int            `json:"total"`
		Items  []spotifyAlbum `json:"items"`
	}
	if err := decodeJSON(body, &providerPage); err != nil || providerPage.Limit < 1 || providerPage.Limit > maximumLimit || providerPage.Offset < 0 || providerPage.Total < 0 || len(providerPage.Items) > providerPage.Limit {
		return AlbumSummaryPage{}, errors.New("invalid album search page")
	}
	page := AlbumSummaryPage{Albums: make([]AlbumSummary, 0, len(providerPage.Items)), Limit: providerPage.Limit, Offset: providerPage.Offset, Total: providerPage.Total}
	for _, item := range providerPage.Items {
		album, err := convertAlbumSummary(item)
		if err != nil {
			return AlbumSummaryPage{}, err
		}
		page.Albums = append(page.Albums, album)
	}
	if page.Offset+page.Limit < page.Total {
		page.NextOffset = page.Offset + page.Limit
	}
	return page, nil
}
