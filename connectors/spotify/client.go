// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package spotify provides bounded Spotify Web API playlist queries.
package spotify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	userAgent                  = "superdurable-dex-spotify-connector/0.1"
	defaultPlaylistPageSize    = 20
	maximumPlaylistPageSize    = 50
	maximumSpotifyIDBytes      = 128
	maximumMetadataStringBytes = 4096
)

var spotifyIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{22}$`)

var errResponseTooLarge = errors.New("Spotify response exceeds the configured size limit")

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient overrides the default HTTP client; the caller retains ownership.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Spotify requests for connector operations.
type Client struct {
	endpoint         *url.URL
	maxResponseBytes int64
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    sdkgo.CredentialRefreshDriver[Credentials]
}

// ListPlaylistTracksInput selects one bounded page of Spotify playlist tracks.
type ListPlaylistTracksInput struct {
	// PlaylistID is the 22-character Spotify playlist identifier.
	PlaylistID string `json:"playlistId"`
	// Market is an optional ISO 3166-1 alpha-2 country code. Spotify prefers the authorized user's country.
	Market string `json:"market,omitempty"`
	// Limit is 1 through 50. Zero selects Spotify's default page size of 20.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based playlist item offset.
	Offset int `json:"offset,omitempty"`
}

// PlaylistTrackPage is one bounded page of playlist tracks in Spotify order.
type PlaylistTrackPage struct {
	// PlaylistID is the requested Spotify playlist identifier.
	PlaylistID string `json:"playlistId"`
	// Tracks contains available track items in playlist order.
	Tracks []PlaylistTrack `json:"tracks"`
	// SkippedItems counts unavailable or non-track items omitted from Tracks.
	SkippedItems int `json:"skippedItems"`
	// Limit is Spotify's page-size value for this response.
	Limit int `json:"limit"`
	// Offset is Spotify's zero-based offset for this response.
	Offset int `json:"offset"`
	// Total is Spotify's current playlist item count.
	Total int `json:"total"`
	// NextOffset is the next page's zero-based offset, or zero when no page follows.
	NextOffset int `json:"nextOffset,omitempty"`
}

// PlaylistTrack is one available Spotify track and its playlist metadata.
type PlaylistTrack struct {
	// Position is the track's zero-based position in the playlist page's current snapshot.
	Position int `json:"position"`
	// AddedAt is when Spotify reports the item was added. Old playlist items may omit it.
	AddedAt *time.Time `json:"addedAt,omitempty"`
	// AddedByUserID is the Spotify user ID that added the item, when available.
	AddedByUserID string `json:"addedByUserId,omitempty"`
	// IsLocal reports that the item refers to a local file instead of Spotify-hosted content.
	IsLocal bool `json:"isLocal"`
	// ID is the Spotify track ID. Local files may omit it.
	ID string `json:"id,omitempty"`
	// URI is Spotify's track or local-file URI.
	URI string `json:"uri,omitempty"`
	// SpotifyURL links back to this track on Spotify when one is available.
	SpotifyURL string `json:"spotifyUrl,omitempty"`
	// Name is Spotify's unmodified track name.
	Name string `json:"name"`
	// Artists are Spotify's track artists in provider order.
	Artists []PlaylistArtist `json:"artists"`
	// Album is the Spotify album on which the track appears.
	Album PlaylistAlbum `json:"album"`
	// DurationMilliseconds is Spotify's track duration in milliseconds.
	DurationMilliseconds int `json:"durationMilliseconds"`
	// Explicit reports Spotify's explicit-content classification.
	Explicit bool `json:"explicit"`
	// IsPlayable is Spotify's market-specific playability when the API includes it.
	IsPlayable *bool `json:"isPlayable,omitempty"`
	// DiscNumber is the track's disc number within the album.
	DiscNumber int `json:"discNumber"`
	// TrackNumber is the track's number on its disc.
	TrackNumber int `json:"trackNumber"`
}

// PlaylistArtist is bounded Spotify artist metadata with an attribution link.
type PlaylistArtist struct {
	// ID is the Spotify artist ID when available.
	ID string `json:"id,omitempty"`
	// URI is the Spotify artist URI when available.
	URI string `json:"uri,omitempty"`
	// SpotifyURL links back to this artist on Spotify when available.
	SpotifyURL string `json:"spotifyUrl,omitempty"`
	// Name is Spotify's unmodified artist name.
	Name string `json:"name"`
}

// PlaylistAlbum is bounded Spotify album metadata with an attribution link.
type PlaylistAlbum struct {
	// ID is the Spotify album ID when available.
	ID string `json:"id,omitempty"`
	// URI is the Spotify album URI when available.
	URI string `json:"uri,omitempty"`
	// SpotifyURL links back to this album on Spotify when available.
	SpotifyURL string `json:"spotifyUrl,omitempty"`
	// Name is Spotify's unmodified album name.
	Name string `json:"name"`
	// ReleaseDate is Spotify's year, year-month, or full release date.
	ReleaseDate string `json:"releaseDate,omitempty"`
	// ReleaseDatePrecision is year, month, or day when Spotify supplies a release date.
	ReleaseDatePrecision string `json:"releaseDatePrecision,omitempty"`
	// Artists are Spotify's album artists in provider order.
	Artists []PlaylistArtist `json:"artists"`
}

// ListPlaylistTracksOperation implements the list playlist tracks connector operation.
type ListPlaylistTracksOperation struct{ client *Client }

type spotifyPlaylistPage struct {
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
	Total  int                   `json:"total"`
	Next   *string               `json:"next"`
	Items  []spotifyPlaylistItem `json:"items"`
}

type spotifyPlaylistItem struct {
	AddedAt *time.Time    `json:"added_at"`
	AddedBy *spotifyUser  `json:"added_by"`
	IsLocal bool          `json:"is_local"`
	Item    *spotifyTrack `json:"item"`
}

type spotifyUser struct {
	ID string `json:"id"`
}

type spotifyTrack struct {
	ID           string              `json:"id"`
	URI          string              `json:"uri"`
	ExternalURLs spotifyExternalURLs `json:"external_urls"`
	Name         string              `json:"name"`
	Type         string              `json:"type"`
	Artists      []spotifyArtist     `json:"artists"`
	Album        *spotifyAlbum       `json:"album"`
	Duration     int                 `json:"duration_ms"`
	Explicit     bool                `json:"explicit"`
	IsPlayable   *bool               `json:"is_playable"`
	IsLocal      bool                `json:"is_local"`
	DiscNumber   int                 `json:"disc_number"`
	TrackNumber  int                 `json:"track_number"`
}

type spotifyArtist struct {
	ID           string              `json:"id"`
	URI          string              `json:"uri"`
	ExternalURLs spotifyExternalURLs `json:"external_urls"`
	Name         string              `json:"name"`
}

type spotifyAlbum struct {
	ID                   string              `json:"id"`
	URI                  string              `json:"uri"`
	ExternalURLs         spotifyExternalURLs `json:"external_urls"`
	Name                 string              `json:"name"`
	ReleaseDate          string              `json:"release_date"`
	ReleaseDatePrecision string              `json:"release_date_precision"`
	Artists              []spotifyArtist     `json:"artists"`
}

type spotifyExternalURLs struct {
	Spotify string `json:"spotify"`
}

type providerResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

// New validates configuration and constructs an authenticated Spotify client.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("Spotify endpoint must be absolute")
	}
	if endpoint.Scheme != "https" && !isLoopback(endpoint.Hostname()) {
		return nil, fmt.Errorf("non-loopback Spotify endpoint must use HTTPS")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, fmt.Errorf("Spotify endpoint cannot contain user info, a query, or a fragment")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Spotify connector option is nil")
		}
		option(&dependencies)
	}
	httpClient := dependencies.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: config.Timeout}
	} else {
		clientCopy := *httpClient
		httpClient = &clientCopy
		if httpClient.Timeout == 0 {
			httpClient.Timeout = config.Timeout
		}
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		endpoint: endpoint, maxResponseBytes: config.MaxResponseBytes,
		httpClient: httpClient, credentials: credentials, refreshDriver: NewCredentialRefreshDriver(httpClient),
	}, nil
}

// ListPlaylistTracks returns the ListPlaylistTracks operation bound to this client.
func (client *Client) ListPlaylistTracks() ListPlaylistTracksOperation {
	return ListPlaylistTracksOperation{client: client}
}

// Definition returns the immutable connector operation definition.
func (ListPlaylistTracksOperation) Definition() sdkgo.QueryDefinition {
	return ListPlaylistTracksDefinition
}

// Invoke executes one provider call and classifies its attempt.
func (operation ListPlaylistTracksOperation) Invoke(call sdkgo.Call, input ListPlaylistTracksInput) sdkgo.QueryAttempt[PlaylistTrackPage] {
	playlistID, market, limit, offset, failure := validatePlaylistRequest(input)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListPlaylistTracksBranchDefect, PlaylistTrackPage{}, failure, sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListPlaylistTracksBranchDefect, PlaylistTrackPage{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.getPlaylistTracks(call, &credential, playlistID, market, limit, offset)
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			failure := spotifyFailure(sdkgo.FailureResponseTooLarge, "Spotify playlist response exceeds the configured size limit")
			return sdkgo.NewQueryBranch(ListPlaylistTracksBranchInvalidResponse, PlaylistTrackPage{}, &failure, sdkgo.Receipt{})
		}
		return sdkgo.NewQueryRetry[PlaylistTrackPage](spotifyFailure(sdkgo.FailureAvailability, "Spotify is unavailable"), 0)
	}
	if attempt := classifyResponse(response); attempt != nil {
		return *attempt
	}
	page, err := decodePlaylistPage(response.body, playlistID)
	if err != nil {
		failure := spotifyFailure(sdkgo.FailureProtocol, "Spotify returned an invalid playlist response")
		return sdkgo.NewQueryBranch(ListPlaylistTracksBranchInvalidResponse, PlaylistTrackPage{}, &failure, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(ListPlaylistTracksBranchListed, page, nil, sdkgo.Receipt{})
}

func (client *Client) resolveCredential(call sdkgo.Call) (Credentials, *sdkgo.Failure) {
	credential, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credential) != nil {
		failure := spotifyFailure(sdkgo.FailureAuthentication, "Spotify authorization is unavailable or revoked")
		return Credentials{}, &failure
	}
	return credential, nil
}

func (client *Client) getPlaylistTracks(
	call sdkgo.Call,
	credential *Credentials,
	playlistID string,
	market string,
	limit int,
	offset int,
) (providerResponse, error) {
	requestURL := strings.TrimRight(client.endpoint.String(), "/") + "/playlists/" + url.PathEscape(playlistID) + "/items"
	query := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if market != "" {
		query.Set("market", market)
	}
	requestURL += "?" + query.Encode()
	for attempt := 0; attempt < 2; attempt++ {
		request, err := http.NewRequestWithContext(call.Context, http.MethodGet, requestURL, nil)
		if err != nil {
			return providerResponse{}, err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
		request.Header.Set("User-Agent", userAgent)
		response, err := client.httpClient.Do(request)
		if err != nil {
			return providerResponse{}, err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			return providerResponse{}, errors.Join(readErr, closeErr)
		}
		result := providerResponse{statusCode: response.StatusCode, header: response.Header.Clone(), body: body}
		if int64(len(body)) > client.maxResponseBytes {
			result.body = nil
			return result, errResponseTooLarge
		}
		if response.StatusCode != http.StatusUnauthorized || attempt != 0 {
			return result, nil
		}
		if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
			return result, nil
		}
		replacement, err := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if err != nil || validateResolvedCredentials(replacement) != nil {
			return result, nil
		}
		*credential = replacement
	}
	return providerResponse{}, errors.New("Spotify authenticated request retry was exhausted")
}

func validateResolvedCredentials(credentials Credentials) error {
	if credentials.AccessToken.Reveal() == "" {
		return errors.New("Spotify access token is required")
	}
	return nil
}

func classifyResponse(response providerResponse) *sdkgo.QueryAttempt[PlaylistTrackPage] {
	if response.statusCode >= 200 && response.statusCode < 300 {
		return nil
	}
	if response.statusCode == http.StatusTooManyRequests {
		attempt := sdkgo.NewQueryRetry[PlaylistTrackPage](
			spotifyFailure(sdkgo.FailureRateLimit, "Spotify rate limit was reached"), retryDelay(response.header),
		)
		return &attempt
	}
	if response.statusCode >= 500 {
		attempt := sdkgo.NewQueryRetry[PlaylistTrackPage](
			spotifyFailure(sdkgo.FailureAvailability, "Spotify is unavailable"), 0,
		)
		return &attempt
	}
	var branch sdkgo.BranchID
	var failure sdkgo.Failure
	switch response.statusCode {
	case http.StatusUnauthorized:
		branch = ListPlaylistTracksBranchAuthorizationRevoked
		failure = spotifyFailure(sdkgo.FailureAuthentication, "Spotify authorization is invalid or revoked")
	case http.StatusForbidden:
		branch = ListPlaylistTracksBranchAccessDenied
		failure = spotifyFailure(sdkgo.FailureAuthorization, "Spotify denied access to the playlist items")
	case http.StatusNotFound:
		branch = ListPlaylistTracksBranchNotFound
		failure = spotifyFailure(sdkgo.FailureNotFound, "Spotify playlist was not found")
	default:
		branch = ListPlaylistTracksBranchProviderRejected
		failure = spotifyFailure(sdkgo.FailureProviderRejection, "Spotify rejected the playlist query")
	}
	attempt := sdkgo.NewQueryBranch(branch, PlaylistTrackPage{}, &failure, sdkgo.Receipt{})
	return &attempt
}

func validatePlaylistRequest(input ListPlaylistTracksInput) (string, string, int, int, *sdkgo.Failure) {
	playlistID := strings.TrimSpace(input.PlaylistID)
	market := strings.ToUpper(strings.TrimSpace(input.Market))
	limit := input.Limit
	if limit == 0 {
		limit = defaultPlaylistPageSize
	}
	if !spotifyIDPattern.MatchString(playlistID) {
		failure := spotifyFailure(sdkgo.FailureValidation, "playlist ID must be a 22-character Spotify ID")
		return "", "", 0, 0, &failure
	}
	if market != "" && (len(market) != 2 || strings.IndexFunc(market, func(character rune) bool {
		return character < 'A' || character > 'Z'
	}) != -1) {
		failure := spotifyFailure(sdkgo.FailureValidation, "market must be an ISO 3166-1 alpha-2 country code")
		return "", "", 0, 0, &failure
	}
	if limit < 1 || limit > maximumPlaylistPageSize {
		failure := spotifyFailure(sdkgo.FailureValidation, "limit must be between 1 and 50")
		return "", "", 0, 0, &failure
	}
	if input.Offset < 0 || input.Offset > math.MaxInt32 {
		failure := spotifyFailure(sdkgo.FailureValidation, "offset must be between 0 and 2147483647")
		return "", "", 0, 0, &failure
	}
	return playlistID, market, limit, input.Offset, nil
}

func decodePlaylistPage(body []byte, playlistID string) (PlaylistTrackPage, error) {
	var providerPage spotifyPlaylistPage
	if err := decodeJSON(body, &providerPage); err != nil {
		return PlaylistTrackPage{}, err
	}
	if providerPage.Limit < 1 || providerPage.Limit > maximumPlaylistPageSize || providerPage.Offset < 0 ||
		providerPage.Total < 0 || providerPage.Items == nil || len(providerPage.Items) > providerPage.Limit {
		return PlaylistTrackPage{}, fmt.Errorf("invalid Spotify playlist page")
	}
	page := PlaylistTrackPage{
		PlaylistID: playlistID, Tracks: make([]PlaylistTrack, 0, len(providerPage.Items)),
		Limit: providerPage.Limit, Offset: providerPage.Offset, Total: providerPage.Total,
	}
	for index, item := range providerPage.Items {
		track, isAvailable, err := convertPlaylistTrack(item, providerPage.Offset+index)
		if err != nil {
			return PlaylistTrackPage{}, err
		}
		if !isAvailable {
			page.SkippedItems++
			continue
		}
		page.Tracks = append(page.Tracks, track)
	}
	if providerPage.Next != nil {
		if *providerPage.Next == "" || len(providerPage.Items) == 0 || providerPage.Offset > math.MaxInt32-len(providerPage.Items) {
			return PlaylistTrackPage{}, fmt.Errorf("invalid Spotify next page")
		}
		page.NextOffset = providerPage.Offset + len(providerPage.Items)
	}
	return page, nil
}

func convertPlaylistTrack(item spotifyPlaylistItem, position int) (PlaylistTrack, bool, error) {
	if item.Item == nil || item.Item.Type != "track" {
		return PlaylistTrack{}, false, nil
	}
	providerTrack := item.Item
	isLocal := item.IsLocal || providerTrack.IsLocal
	if err := validateSpotifyTrack(providerTrack, isLocal); err != nil {
		return PlaylistTrack{}, false, err
	}
	trackURL, err := spotifyURL(providerTrack.ExternalURLs.Spotify)
	if err != nil {
		return PlaylistTrack{}, false, err
	}
	trackURI, err := spotifyURI(providerTrack.URI)
	if err != nil {
		return PlaylistTrack{}, false, err
	}
	artists, err := convertArtists(providerTrack.Artists)
	if err != nil {
		return PlaylistTrack{}, false, err
	}
	album, err := convertAlbum(*providerTrack.Album)
	if err != nil {
		return PlaylistTrack{}, false, err
	}
	addedByUserID := ""
	if item.AddedBy != nil {
		addedByUserID = item.AddedBy.ID
		if addedByUserID != "" && !validSpotifyIdentifier(addedByUserID) {
			return PlaylistTrack{}, false, fmt.Errorf("invalid Spotify user ID")
		}
	}
	return PlaylistTrack{
		Position: position, AddedAt: item.AddedAt, AddedByUserID: addedByUserID, IsLocal: isLocal,
		ID: providerTrack.ID, URI: trackURI, SpotifyURL: trackURL, Name: providerTrack.Name,
		Artists: artists, Album: album, DurationMilliseconds: providerTrack.Duration,
		Explicit: providerTrack.Explicit, IsPlayable: providerTrack.IsPlayable,
		DiscNumber: providerTrack.DiscNumber, TrackNumber: providerTrack.TrackNumber,
	}, true, nil
}

func validateSpotifyTrack(track *spotifyTrack, isLocal bool) error {
	if track.Album == nil || track.Artists == nil || track.Duration < 0 || track.DiscNumber < 0 || track.TrackNumber < 0 {
		return fmt.Errorf("invalid Spotify track")
	}
	if err := validateMetadataString(track.Name, "track name", true); err != nil {
		return err
	}
	if track.ID != "" && !spotifyIDPattern.MatchString(track.ID) {
		return fmt.Errorf("invalid Spotify track ID")
	}
	if !isLocal && track.ID == "" {
		return fmt.Errorf("Spotify track ID is missing")
	}
	return nil
}

func convertArtists(providerArtists []spotifyArtist) ([]PlaylistArtist, error) {
	artists := make([]PlaylistArtist, 0, len(providerArtists))
	for _, providerArtist := range providerArtists {
		if err := validateMetadataString(providerArtist.Name, "artist name", true); err != nil {
			return nil, err
		}
		if providerArtist.ID != "" && !spotifyIDPattern.MatchString(providerArtist.ID) {
			return nil, fmt.Errorf("invalid Spotify artist ID")
		}
		artistURL, err := spotifyURL(providerArtist.ExternalURLs.Spotify)
		if err != nil {
			return nil, err
		}
		artistURI, err := spotifyURI(providerArtist.URI)
		if err != nil {
			return nil, err
		}
		artists = append(artists, PlaylistArtist{
			ID: providerArtist.ID, URI: artistURI, SpotifyURL: artistURL, Name: providerArtist.Name,
		})
	}
	return artists, nil
}

func convertAlbum(providerAlbum spotifyAlbum) (PlaylistAlbum, error) {
	if err := validateMetadataString(providerAlbum.Name, "album name", true); err != nil {
		return PlaylistAlbum{}, err
	}
	if providerAlbum.ID != "" && !spotifyIDPattern.MatchString(providerAlbum.ID) {
		return PlaylistAlbum{}, fmt.Errorf("invalid Spotify album ID")
	}
	if providerAlbum.Artists == nil {
		return PlaylistAlbum{}, fmt.Errorf("Spotify album artists are missing")
	}
	albumURL, err := spotifyURL(providerAlbum.ExternalURLs.Spotify)
	if err != nil {
		return PlaylistAlbum{}, err
	}
	albumURI, err := spotifyURI(providerAlbum.URI)
	if err != nil {
		return PlaylistAlbum{}, err
	}
	artists, err := convertArtists(providerAlbum.Artists)
	if err != nil {
		return PlaylistAlbum{}, err
	}
	if err := validateMetadataString(providerAlbum.ReleaseDate, "album release date", false); err != nil {
		return PlaylistAlbum{}, err
	}
	if providerAlbum.ReleaseDatePrecision != "" && providerAlbum.ReleaseDatePrecision != "year" &&
		providerAlbum.ReleaseDatePrecision != "month" && providerAlbum.ReleaseDatePrecision != "day" {
		return PlaylistAlbum{}, fmt.Errorf("invalid Spotify release date precision")
	}
	return PlaylistAlbum{
		ID: providerAlbum.ID, URI: albumURI, SpotifyURL: albumURL, Name: providerAlbum.Name,
		ReleaseDate: providerAlbum.ReleaseDate, ReleaseDatePrecision: providerAlbum.ReleaseDatePrecision,
		Artists: artists,
	}, nil
}

func validateMetadataString(value string, field string, required bool) error {
	if required && value == "" {
		return fmt.Errorf("Spotify %s is missing", field)
	}
	if len(value) > maximumMetadataStringBytes || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) != -1 {
		return fmt.Errorf("invalid Spotify %s", field)
	}
	return nil
}

func validSpotifyIdentifier(value string) bool {
	return len(value) <= maximumSpotifyIDBytes && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) == -1
}

func spotifyURL(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > 2048 || !utf8.ValidString(value) {
		return "", fmt.Errorf("invalid Spotify URL")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "open.spotify.com") || parsed.User != nil {
		return "", fmt.Errorf("invalid Spotify URL")
	}
	return parsed.String(), nil
}

func spotifyURI(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > 2048 || !utf8.ValidString(value) || !strings.HasPrefix(value, "spotify:") ||
		strings.IndexFunc(value, unicode.IsControl) != -1 {
		return "", fmt.Errorf("invalid Spotify URI")
	}
	return value, nil
}

func spotifyFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "spotify", Operation: "listPlaylistTracks", Message: message}
}

func retryDelay(header http.Header) time.Duration {
	seconds, err := strconv.ParseInt(strings.TrimSpace(header.Get("Retry-After")), 10, 64)
	if err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Minute
}

func decodeJSON(body []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("response contains trailing JSON")
	}
	return nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
