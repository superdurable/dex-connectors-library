// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spotify_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/spotify"
	"github.com/superdurable/dex-connectors-library/connectors/spotify/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testPlaylistID = "3cEYpjA9oz9GiPac4AsH4n"
	testTrackID    = "4iV5W9uYEdYUVa79Axb7Rh"
	testArtistID   = "0OdUWJ0sBjDrqHygGUXeCF"
	testAlbumID    = "1ATL5GLyefJaxhQzSPVrLX"
)

var spotifyConnection = sdkgo.ConnectionRef{Provider: "spotify", Name: "playlist-reader"}

type rejectionRefreshingCredentialProvider struct {
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (spotify.Credentials, error) {
	return spotify.Credentials{AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[spotify.Credentials],
) (spotify.Credentials, error) {
	provider.forcedRefreshes++
	return spotify.Credentials{AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

func TestListPlaylistTracksReturnsBoundedProviderMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/playlists/"+testPlaylistID+"/items", request.URL.Path)
		require.Equal(t, "2", request.URL.Query().Get("limit"))
		require.Equal(t, "5", request.URL.Query().Get("offset"))
		require.Equal(t, "US", request.URL.Query().Get("market"))
		require.Equal(t, "Bearer one-use-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		writeJSON(t, response, map[string]any{
			"limit": 2, "offset": 5, "total": 9,
			"next": serverURL(request) + "/v1/playlists/" + testPlaylistID + "/items?offset=7&limit=2",
			"items": []any{
				playlistItem(),
				map[string]any{"added_at": nil, "added_by": nil, "is_local": false, "item": nil},
			},
		})
	}))
	defer server.Close()

	client := newClient(t, server.URL+"/v1", spotify.Config{})
	result, err := runList(t, client, spotify.ListPlaylistTracksInput{
		PlaylistID: testPlaylistID, Market: " us ", Limit: 2, Offset: 5,
	})
	require.NoError(t, err)
	require.Equal(t, spotify.ListPlaylistTracksBranchListed, result.Branch)
	require.Equal(t, testPlaylistID, result.Value.PlaylistID)
	require.Equal(t, 2, result.Value.Limit)
	require.Equal(t, 5, result.Value.Offset)
	require.Equal(t, 9, result.Value.Total)
	require.Equal(t, 7, result.Value.NextOffset)
	require.Equal(t, 1, result.Value.SkippedItems)
	require.Len(t, result.Value.Tracks, 1)
	track := result.Value.Tracks[0]
	require.Equal(t, 5, track.Position)
	require.Equal(t, testTrackID, track.ID)
	require.Equal(t, "spotify:track:"+testTrackID, track.URI)
	require.Equal(t, "https://open.spotify.com/track/"+testTrackID, track.SpotifyURL)
	require.Equal(t, "A Song", track.Name)
	require.Equal(t, 213_000, track.DurationMilliseconds)
	require.True(t, track.Explicit)
	require.NotNil(t, track.IsPlayable)
	require.True(t, *track.IsPlayable)
	require.Equal(t, "playlist-owner", track.AddedByUserID)
	require.Equal(t, time.Date(2026, 9, 1, 12, 30, 0, 0, time.UTC), *track.AddedAt)
	require.Equal(t, "An Artist", track.Artists[0].Name)
	require.Equal(t, "An Album", track.Album.Name)
	require.Equal(t, "2026-08-14", track.Album.ReleaseDate)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "one-use-token")
	require.NotContains(t, fmt.Sprintf("%#v", result), "provider-secret-body")
}

func TestListPlaylistTracksUsesSpotifyDefaultPageSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "20", request.URL.Query().Get("limit"))
		require.Equal(t, "0", request.URL.Query().Get("offset"))
		require.Empty(t, request.URL.Query().Get("market"))
		writeJSON(t, response, map[string]any{
			"limit": 20, "offset": 0, "total": 0, "next": nil, "items": []any{},
		})
	}))
	defer server.Close()

	result, err := runList(t, newClient(t, server.URL, spotify.Config{}), spotify.ListPlaylistTracksInput{PlaylistID: testPlaylistID})
	require.NoError(t, err)
	require.Equal(t, spotify.ListPlaylistTracksBranchListed, result.Branch)
	require.Empty(t, result.Value.Tracks)
	require.Zero(t, result.Value.NextOffset)
}

func TestListPlaylistTracksRefreshesRejectedCredentialOnce(t *testing.T) {
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := providerCalls.Add(1)
		if call == 1 {
			require.Equal(t, "Bearer rejected-token", request.Header.Get("Authorization"))
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		require.Equal(t, int32(2), call)
		require.Equal(t, "Bearer replacement-token", request.Header.Get("Authorization"))
		writeJSON(t, response, map[string]any{
			"limit": 20, "offset": 0, "total": 0, "next": nil, "items": []any{},
		})
	}))
	defer server.Close()

	provider := &rejectionRefreshingCredentialProvider{}
	client, err := spotify.New(spotify.Config{Endpoint: server.URL}, provider)
	require.NoError(t, err)
	result, err := runList(t, client, spotify.ListPlaylistTracksInput{PlaylistID: testPlaylistID})
	require.NoError(t, err)
	require.Equal(t, spotify.ListPlaylistTracksBranchListed, result.Branch)
	require.Equal(t, 1, provider.forcedRefreshes)
	require.Equal(t, int32(2), providerCalls.Load())
}

func TestListPlaylistTracksRejectsInvalidInputBeforeProviderAccess(t *testing.T) {
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		providerCalls.Add(1)
	}))
	defer server.Close()
	client := newClient(t, server.URL, spotify.Config{})

	for name, input := range map[string]spotify.ListPlaylistTracksInput{
		"missing playlist": {},
		"playlist URL":     {PlaylistID: "https://open.spotify.com/playlist/" + testPlaylistID},
		"invalid market":   {PlaylistID: testPlaylistID, Market: "USA"},
		"negative limit":   {PlaylistID: testPlaylistID, Limit: -1},
		"large limit":      {PlaylistID: testPlaylistID, Limit: 51},
		"negative offset":  {PlaylistID: testPlaylistID, Offset: -1},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := runList(t, client, input)
			require.NoError(t, err)
			require.Equal(t, spotify.ListPlaylistTracksBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Zero(t, providerCalls.Load())
}

func TestListPlaylistTracksClassifiesTerminalProviderResponses(t *testing.T) {
	tests := []struct {
		status int
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{http.StatusUnauthorized, spotify.ListPlaylistTracksBranchAuthorizationRevoked, sdkgo.FailureAuthentication},
		{http.StatusForbidden, spotify.ListPlaylistTracksBranchAccessDenied, sdkgo.FailureAuthorization},
		{http.StatusNotFound, spotify.ListPlaylistTracksBranchNotFound, sdkgo.FailureNotFound},
		{http.StatusBadRequest, spotify.ListPlaylistTracksBranchProviderRejected, sdkgo.FailureProviderRejection},
		{http.StatusFound, spotify.ListPlaylistTracksBranchProviderRejected, sdkgo.FailureProviderRejection},
	}
	for _, testCase := range tests {
		t.Run(fmt.Sprintf("status-%d", testCase.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(testCase.status)
				_, _ = response.Write([]byte(`{"error":{"message":"provider-secret-body"}}`))
			}))
			defer server.Close()
			result, err := runList(t, newClient(t, server.URL, spotify.Config{}), spotify.ListPlaylistTracksInput{PlaylistID: testPlaylistID})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.NotContains(t, fmt.Sprintf("%#v", result), "provider-secret-body")
		})
	}
}

func TestListPlaylistTracksRetriesRateLimitsAvailabilityAndTransportFailures(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		delay  time.Duration
		kind   sdkgo.FailureKind
	}{
		{name: "rate limit", status: http.StatusTooManyRequests, delay: 7 * time.Second, kind: sdkgo.FailureRateLimit},
		{name: "rate limit default", status: http.StatusTooManyRequests, delay: time.Minute, kind: sdkgo.FailureRateLimit},
		{name: "unavailable", status: http.StatusServiceUnavailable, kind: sdkgo.FailureAvailability},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				if testCase.delay == 7*time.Second {
					response.Header().Set("Retry-After", "7")
				}
				response.WriteHeader(testCase.status)
			}))
			defer server.Close()
			_, err := runList(t, newClient(t, server.URL, spotify.Config{}), spotify.ListPlaylistTracksInput{PlaylistID: testPlaylistID})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, testCase.kind, retry.Failure.Kind)
			if testCase.delay > 0 {
				var retryAfter *dex.RetryAfterError
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, testCase.delay, retryAfter.After)
			}
		})
	}

	transportClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer one-use-token", request.Header.Get("Authorization"))
		return nil, errors.New("transport failed with one-use-token")
	})}
	client := newClient(t, "https://api.spotify.test/v1", spotify.Config{}, spotify.WithHTTPClient(transportClient))
	_, err := runList(t, client, spotify.ListPlaylistTracksInput{PlaylistID: testPlaylistID})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.NotContains(t, err.Error(), "one-use-token")
}

func TestListPlaylistTracksRejectsInvalidAndOversizedResponses(t *testing.T) {
	for name, body := range map[string]string{
		"malformed":        `{`,
		"trailing":         `{"limit":20,"offset":0,"total":0,"next":null,"items":[]} trailing`,
		"missing items":    `{"limit":20,"offset":0,"total":0,"next":null}`,
		"too many items":   playlistPageJSON(1, []any{playlistItem(), playlistItem()}),
		"missing track id": playlistPageJSON(20, []any{playlistItemWith(func(track map[string]any) { track["id"] = "" })}),
		"invalid track URL": playlistPageJSON(20, []any{playlistItemWith(func(track map[string]any) {
			track["external_urls"] = map[string]any{"spotify": "https://evil.example/track/id"}
		})}),
		"empty next page": `{"limit":20,"offset":0,"total":10,"next":"https://api.spotify.com/next","items":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte(body))
			}))
			defer server.Close()
			result, err := runList(t, newClient(t, server.URL, spotify.Config{}), spotify.ListPlaylistTracksInput{PlaylistID: testPlaylistID})
			require.NoError(t, err)
			require.Equal(t, spotify.ListPlaylistTracksBranchInvalidResponse, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer server.Close()
	result, err := runList(t, newClient(t, server.URL, spotify.Config{MaxResponseBytes: 16}), spotify.ListPlaylistTracksInput{PlaylistID: testPlaylistID})
	require.NoError(t, err)
	require.Equal(t, spotify.ListPlaylistTracksBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestListPlaylistTracksMissingConnectionUsesDefectBranch(t *testing.T) {
	client, err := spotify.New(spotify.Config{}, sdkgo.StaticCredentialProvider[spotify.Credentials]{})
	require.NoError(t, err)
	result, err := runList(t, client, spotify.ListPlaylistTracksInput{PlaylistID: testPlaylistID})
	require.NoError(t, err)
	require.Equal(t, spotify.ListPlaylistTracksBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
}

func TestNewRejectsUnsafeSpotifyEndpoints(t *testing.T) {
	credentials := testCredentials()
	_, err := spotify.New(spotify.Config{Endpoint: "http://api.spotify.test/v1"}, credentials)
	require.ErrorContains(t, err, "must use HTTPS")
	_, err = spotify.New(spotify.Config{Endpoint: "https://token@api.spotify.com/v1"}, credentials)
	require.ErrorContains(t, err, "cannot contain user info")
	_, err = spotify.New(spotify.Config{Endpoint: "https://api.spotify.com/v1?token=value"}, credentials)
	require.ErrorContains(t, err, "cannot contain user info")
}

func newClient(t *testing.T, endpoint string, config spotify.Config, options ...spotify.Option) *spotify.Client {
	t.Helper()
	config.Endpoint = endpoint
	client, err := spotify.New(config, testCredentials(), options...)
	require.NoError(t, err)
	return client
}

func testCredentials() sdkgo.StaticCredentialProvider[spotify.Credentials] {
	return sdkgo.StaticCredentialProvider[spotify.Credentials]{spotifyConnection: {
		AccessToken: sdkgo.NewSecretString("one-use-token"),
	}}
}

func runList(t *testing.T, client *spotify.Client, input spotify.ListPlaylistTracksInput) (sdkgo.QueryResult[spotify.PlaylistTrackPage], error) {
	t.Helper()
	return sdkgo.RunQuery(
		testsupport.NewDexContext("playlist-flow", "list-tracks-step"),
		client.ListPlaylistTracks(), spotifyConnection, input,
	)
}

func playlistItem() map[string]any {
	return playlistItemWith(nil)
}

func playlistItemWith(change func(map[string]any)) map[string]any {
	artist := map[string]any{
		"id": testArtistID, "uri": "spotify:artist:" + testArtistID, "name": "An Artist",
		"external_urls": map[string]any{"spotify": "https://open.spotify.com/artist/" + testArtistID},
	}
	track := map[string]any{
		"id": testTrackID, "uri": "spotify:track:" + testTrackID, "name": "A Song", "type": "track",
		"external_urls": map[string]any{"spotify": "https://open.spotify.com/track/" + testTrackID},
		"artists":       []any{artist}, "duration_ms": 213000, "explicit": true, "is_playable": true,
		"is_local": false, "disc_number": 1, "track_number": 2,
		"album": map[string]any{
			"id": testAlbumID, "uri": "spotify:album:" + testAlbumID, "name": "An Album",
			"external_urls": map[string]any{"spotify": "https://open.spotify.com/album/" + testAlbumID},
			"release_date":  "2026-08-14", "release_date_precision": "day", "artists": []any{artist},
		},
	}
	if change != nil {
		change(track)
	}
	return map[string]any{
		"added_at": "2026-09-01T12:30:00Z", "added_by": map[string]any{"id": "playlist-owner"},
		"is_local": false, "item": track,
	}
}

func playlistPageJSON(limit int, items []any) string {
	contents, err := json.Marshal(map[string]any{
		"limit": limit, "offset": 0, "total": len(items), "next": nil, "items": items,
	})
	if err != nil {
		panic(err)
	}
	return string(contents)
}

func serverURL(request *http.Request) string {
	return "http://" + request.Host
}

func writeJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(response).Encode(value))
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
