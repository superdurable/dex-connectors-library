//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package playlistcurator

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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	youtube "github.com/superdurable/dex-connectors-library/connectors/google/youtube"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationToken       = "ya29.integration-token"
	integrationManageToken = "ya29.integration-manage-token"
	integrationChannelID   = "UC_x5XG1OV2P6uZZ5FSM9Ttw"
)

func TestPlaylistCuratorAddsOnceWithRealDex(t *testing.T) {
	provider := newYouTubeProvider(t)
	flow, harness := newCuratorHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)

	t.Run("an ambiguous insert retries and adds the video once", func(t *testing.T) {
		playlistID := "PLflaky" + runID
		flowID := "youtube-curator-added-" + runID
		result := harness.runFlow(t, ctx, flow, flowID, CurationRequest{Channel: "@googledevelopers", Query: "release", PlaylistID: playlistID})
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var outcome CurationOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, StatusAdded, outcome.Status)
		require.Equal(t, integrationChannelID, outcome.ChannelID)
		require.Equal(t, "dQw4w9WgXcQ", outcome.VideoID)
		require.True(t, outcome.IsConfirmed)
		require.True(t, outcome.WasAlreadyPresent, "the retried attempt found the item the failed insert stored")
		require.Equal(t, 1, provider.insertsFor(playlistID))
		for _, call := range provider.tokenLog() {
			if strings.HasPrefix(call, "GET /channels ") || strings.HasPrefix(call, "GET /search ") {
				require.True(t, strings.HasSuffix(call, " "+integrationToken), "reads use the read-only connection: %s", call)
			}
			if strings.HasPrefix(call, "POST ") {
				require.True(t, strings.HasSuffix(call, " "+integrationManageToken), "the insert uses the manage connection: %s", call)
			}
		}

		var display map[string]any
		require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.GetDexDisplay, nil, &display))
		encoded, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encoded), outcome.PlaylistItemID)
		require.NotContains(t, string(encoded), integrationToken)
		require.NotContains(t, string(encoded), integrationManageToken)
	})

	t.Run("an unknown handle completes without changes", func(t *testing.T) {
		playlistID := "PLmissing" + runID
		result := harness.runFlow(t, ctx, flow, "youtube-curator-missing-"+runID, CurationRequest{Channel: "@missing", Query: "release", PlaylistID: playlistID})
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var outcome CurationOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, StatusChannelNotFound, outcome.Status)
		require.Zero(t, provider.insertsFor(playlistID))
	})

	t.Run("no matching video completes without changes", func(t *testing.T) {
		playlistID := "PLnomatch" + runID
		result := harness.runFlow(t, ctx, flow, "youtube-curator-nomatch-"+runID, CurationRequest{Channel: integrationChannelID, Query: "nothing", PlaylistID: playlistID})
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var outcome CurationOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, StatusNoMatch, outcome.Status)
		require.Zero(t, provider.insertsFor(playlistID))
	})

	t.Run("invalid start input fails before calling YouTube", func(t *testing.T) {
		before := provider.requestCount()
		result := harness.runFlow(t, ctx, flow, "youtube-curator-invalid-"+runID, CurationRequest{Channel: "@a"})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Equal(t, before, provider.requestCount())
	})
}

// youtubeProvider is a stateful local YouTube Data API. Each playlist's first insert stores the item and answers HTTP 503.
type youtubeProvider struct {
	*httptest.Server
	mutex    sync.Mutex
	requests int
	tokens   []string
	items    map[string][]string
	inserts  map[string]int
}

func newYouTubeProvider(t *testing.T) *youtubeProvider {
	t.Helper()
	provider := &youtubeProvider{items: map[string][]string{}, inserts: map[string]int{}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func playlistItemJSON(playlistID string, position int, videoID string) string {
	return fmt.Sprintf(`{"id":"UExpdGVt%d","snippet":{"playlistId":%q,"title":"Release notes","position":%d,"publishedAt":"2026-10-13T17:00:00Z",
		"resourceId":{"kind":"youtube#video","videoId":%q}},"contentDetails":{"videoPublishedAt":"2026-10-01T16:00:00Z"}}`, position, playlistID, position, videoID)
}

func (provider *youtubeProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.requests++
	query := request.URL.Query()
	authorization := request.Header.Get("Authorization")
	provider.tokens = append(provider.tokens, request.Method+" "+strings.TrimPrefix(request.URL.Path, "/youtube/v3")+" "+strings.TrimPrefix(authorization, "Bearer "))
	isKnownToken := authorization == "Bearer "+integrationToken || authorization == "Bearer "+integrationManageToken
	if request.Method == http.MethodPost && authorization != "Bearer "+integrationManageToken {
		writeProviderResponse(response, http.StatusForbidden, `{"error":{"errors":[{"reason":"insufficientPermissions"}],"message":"SENTINEL"}}`)
		return
	}
	if !isKnownToken || !strings.HasPrefix(request.URL.Path, "/youtube/v3/") {
		writeProviderResponse(response, http.StatusUnauthorized, `{"error":{"errors":[{"reason":"authError"}],"message":"SENTINEL"}}`)
		return
	}
	switch request.Method + " " + strings.TrimPrefix(request.URL.Path, "/youtube/v3") {
	case "GET /channels":
		if query.Get("forHandle") == "@missing" {
			writeProviderResponse(response, http.StatusOK, `{"kind":"youtube#channelListResponse","pageInfo":{"totalResults":0}}`)
			return
		}
		writeProviderResponse(response, http.StatusOK, fmt.Sprintf(`{"items":[{"id":%q,"snippet":{"title":"Google for Developers","publishedAt":"2007-08-23T00:34:43Z"},
			"contentDetails":{"relatedPlaylists":{"uploads":"UU_x5XG1OV2P6uZZ5FSM9Ttw"}},"statistics":{"hiddenSubscriberCount":true,"videoCount":"10"}}]}`, integrationChannelID))
	case "GET /search":
		if query.Get("q") == "nothing" {
			writeProviderResponse(response, http.StatusOK, `{"pageInfo":{"totalResults":0},"items":[]}`)
			return
		}
		writeProviderResponse(response, http.StatusOK, fmt.Sprintf(`{"pageInfo":{"totalResults":1},"items":[{"id":{"kind":"youtube#video","videoId":"dQw4w9WgXcQ"},
			"snippet":{"channelId":%q,"channelTitle":"Google for Developers","title":"Release notes","description":"","publishedAt":"2026-10-01T16:00:00Z"}}]}`, query.Get("channelId")))
	case "GET /playlistItems":
		playlistID := query.Get("playlistId")
		var matches []string
		for position, videoID := range provider.items[playlistID] {
			if query.Get("videoId") == "" || query.Get("videoId") == videoID {
				matches = append(matches, playlistItemJSON(playlistID, position, videoID))
			}
		}
		writeProviderResponse(response, http.StatusOK, fmt.Sprintf(`{"pageInfo":{"totalResults":%d},"items":[%s]}`, len(matches), strings.Join(matches, ",")))
	case "POST /playlistItems":
		var payload struct {
			Snippet struct {
				PlaylistID string `json:"playlistId"`
				ResourceID struct {
					VideoID string `json:"videoId"`
				} `json:"resourceId"`
			} `json:"snippet"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writeProviderResponse(response, http.StatusBadRequest, `{"error":{"errors":[{"reason":"invalidParameter"}]}}`)
			return
		}
		playlistID := payload.Snippet.PlaylistID
		provider.inserts[playlistID]++
		provider.items[playlistID] = append(provider.items[playlistID], payload.Snippet.ResourceID.VideoID)
		if provider.inserts[playlistID] == 1 {
			writeProviderResponse(response, http.StatusServiceUnavailable, `{"error":{"errors":[{"reason":"backendError"}],"message":"SENTINEL"}}`)
			return
		}
		writeProviderResponse(response, http.StatusOK, playlistItemJSON(playlistID, len(provider.items[playlistID])-1, payload.Snippet.ResourceID.VideoID))
	default:
		writeProviderResponse(response, http.StatusNotFound, `{"error":{"errors":[{"reason":"notFound"}]}}`)
	}
}

func writeProviderResponse(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		panic(err)
	}
}

func (provider *youtubeProvider) insertsFor(playlistID string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.inserts[playlistID]
}

func (provider *youtubeProvider) tokenLog() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.tokens...)
}

func (provider *youtubeProvider) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests
}

type curatorHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newCuratorHarness(t *testing.T, providerURL string) (*Flow, *curatorHarness) {
	t.Helper()
	readReference := sdkgo.ConnectionRef{Provider: "google", Name: ReadConnectionName}
	manageReference := sdkgo.ConnectionRef{Provider: "google", Name: ManageConnectionName}
	providerClient, err := youtube.New(youtube.Config{Endpoint: providerURL + "/youtube/v3"}, sdkgo.StaticCredentialProvider[youtube.Credentials]{
		readReference:   {AuthMethodID: youtube.ReadOnlyAuthMethodID, AccessToken: sdkgo.NewSecretString(integrationToken)},
		manageReference: {AuthMethodID: youtube.ManageAuthMethodID, AccessToken: sdkgo.NewSecretString(integrationManageToken)},
	})
	require.NoError(t, err)
	readConnection, err := youtube.NewConnection(providerClient, readReference)
	require.NoError(t, err)
	manageConnection, err := youtube.NewConnection(providerClient, manageReference)
	require.NoError(t, err)
	flow := NewFlow(readConnection, manageConnection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &curatorHarness{
		registry: registry, cache: cache, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"), workerAddress: workerAddress,
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if harness.worker != nil {
			harness.stopWorker(t)
		}
		require.NoError(t, errors.Join(harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func (harness *curatorHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
}

func (harness *curatorHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func (harness *curatorHarness) runFlow(t *testing.T, ctx context.Context, flow *Flow, flowID string, input CurationRequest) dex.FlowResult {
	t.Helper()
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	return result
}

func availableIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
