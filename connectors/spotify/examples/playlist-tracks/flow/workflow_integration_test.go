//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package playlisttracks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/spotify"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAccessToken = "spotify-SENTINEL-integration-token"
	integrationPlaylistID  = "3cEYpjA9oz9GiPac4AsH4n"
	integrationTrackID     = "4iV5W9uYEdYUVa79Axb7Rh"
	integrationArtistID    = "0OdUWJ0sBjDrqHygGUXeCF"
	integrationAlbumID     = "1ATL5GLyefJaxhQzSPVrLX"
)

// TestPlaylistTracksExampleRetriesAndRoutesWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestPlaylistTracksExampleRetriesAndRoutesWithRealDex(t *testing.T) {
	provider := newFakeSpotifyProvider(t, http.StatusTooManyRequests, http.StatusOK, http.StatusForbidden)
	reference := sdkgo.ConnectionRef{Provider: "spotify", Name: ConnectionName}
	client, err := spotify.New(spotify.Config{Endpoint: provider.server.URL + "/v1"},
		sdkgo.StaticCredentialProvider[spotify.Credentials]{
			reference: {AccessToken: sdkgo.NewSecretString(integrationAccessToken)},
		})
	require.NoError(t, err)
	connection, err := spotify.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	dexClient := startPlaylistWorker(t, flow)

	t.Run("rate-limited query retries and completes", func(t *testing.T) {
		flowID := fmt.Sprintf("spotify-playlist-tracks-retry-%d", time.Now().UnixNano())
		result := runPlaylistFlow(t, dexClient, flow, flowID, Input{PlaylistID: integrationPlaylistID, Limit: 1})
		require.Equal(t, dex.FlowCompleted, result.Status, "Flow failed: %s", result.ErrorMessage)
		var page spotify.PlaylistTrackPage
		require.NoError(t, result.DecodeSingleOutput(&page))
		require.Equal(t, integrationPlaylistID, page.PlaylistID)
		require.Len(t, page.Tracks, 1)
		require.Equal(t, integrationTrackID, page.Tracks[0].ID)
		require.GreaterOrEqual(t, provider.requestGap(0, 1), 900*time.Millisecond, "Dex honored Spotify's Retry-After delay")

		var display map[string]any
		require.NoError(t, dexClient.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encodedDisplay, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encodedDisplay), "A Song")
		require.NotContains(t, string(encodedDisplay), integrationAccessToken)
	})

	t.Run("inaccessible playlist fails through the unwired accessDenied branch", func(t *testing.T) {
		result := runPlaylistFlow(t, dexClient, flow,
			fmt.Sprintf("spotify-playlist-tracks-forbidden-%d", time.Now().UnixNano()),
			Input{PlaylistID: integrationPlaylistID, Limit: 1},
		)
		require.Equal(t, dex.FlowFailed, result.Status)
		require.NotContains(t, result.ErrorMessage, integrationAccessToken)
	})
	require.Equal(t, 3, provider.requestCount(), "Spotify 403 is terminal and is not retried")
}

type fakeSpotifyProvider struct {
	server    *httptest.Server
	mu        sync.Mutex
	statuses  []int
	requested []time.Time
}

func newFakeSpotifyProvider(t *testing.T, statuses ...int) *fakeSpotifyProvider {
	t.Helper()
	provider := &fakeSpotifyProvider{statuses: append([]int(nil), statuses...)}
	provider.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/playlists/"+integrationPlaylistID+"/items", request.URL.Path)
		require.Equal(t, "1", request.URL.Query().Get("limit"))
		require.Equal(t, "Bearer "+integrationAccessToken, request.Header.Get("Authorization"))
		status := provider.nextStatus()
		if status == http.StatusTooManyRequests {
			response.Header().Set("Retry-After", "1")
		}
		if status != http.StatusOK {
			response.WriteHeader(status)
			_, _ = response.Write([]byte(`{"error":{"message":"provider body must not enter the Flow"}}`))
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(integrationPlaylistResponse()))
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

func (provider *fakeSpotifyProvider) nextStatus() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.requested = append(provider.requested, time.Now())
	if len(provider.statuses) == 0 {
		return http.StatusOK
	}
	status := provider.statuses[0]
	provider.statuses = provider.statuses[1:]
	return status
}

func (provider *fakeSpotifyProvider) requestGap(first, second int) time.Duration {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.requested[second].Sub(provider.requested[first])
}

func (provider *fakeSpotifyProvider) requestCount() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return len(provider.requested)
}

func integrationPlaylistResponse() string {
	return `{
  "limit": 1,
  "offset": 0,
  "total": 1,
  "next": null,
  "items": [{
    "added_at": "2026-09-01T12:30:00Z",
    "added_by": {"id": "playlist-owner"},
    "is_local": false,
    "item": {
      "id": "` + integrationTrackID + `",
      "uri": "spotify:track:` + integrationTrackID + `",
      "external_urls": {"spotify": "https://open.spotify.com/track/` + integrationTrackID + `"},
      "name": "A Song",
      "type": "track",
      "duration_ms": 213000,
      "explicit": false,
      "is_playable": true,
      "is_local": false,
      "disc_number": 1,
      "track_number": 2,
      "artists": [{
        "id": "` + integrationArtistID + `",
        "uri": "spotify:artist:` + integrationArtistID + `",
        "external_urls": {"spotify": "https://open.spotify.com/artist/` + integrationArtistID + `"},
        "name": "An Artist"
      }],
      "album": {
        "id": "` + integrationAlbumID + `",
        "uri": "spotify:album:` + integrationAlbumID + `",
        "external_urls": {"spotify": "https://open.spotify.com/album/` + integrationAlbumID + `"},
        "name": "An Album",
        "release_date": "2026-08-14",
        "release_date_precision": "day",
        "artists": [{
          "id": "` + integrationArtistID + `",
          "uri": "spotify:artist:` + integrationArtistID + `",
          "external_urls": {"spotify": "https://open.spotify.com/artist/` + integrationArtistID + `"},
          "name": "An Artist"
        }]
      }
    }
  }]
}`
}

func startPlaylistWorker(t *testing.T, flow *Flow) *dex.Client {
	t.Helper()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	workerAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
		WorkerTarget:       &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, client.Close(), cache.Close()))
	})
	return client
}

func runPlaylistFlow(t *testing.T, client *dex.Client, flow *Flow, flowID string, input Input) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	for {
		result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err)
		return result
	}
}
