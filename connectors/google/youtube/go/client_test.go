// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	youtube "github.com/superdurable/dex-connectors-library/connectors/google/youtube"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testChannelID  = "UC_x5XG1OV2P6uZZ5FSM9Ttw"
	testVideoID    = "dQw4w9WgXcQ"
	testPlaylistID = "PLexample123"
)

var youtubeConnection = sdkgo.ConnectionRef{Provider: "google", Name: "youtube"}

// fakeYouTube is a stateful YouTube Data API v3 fake for one playlist.
type fakeYouTube struct {
	t            *testing.T
	mutex        sync.Mutex
	items        []map[string]any
	inserts      int
	failInsert   int // status returned by the insert after it stores the item
	errorStatus  int
	errorReason  string
	lastQuery    map[string]string
	requestCount int
}

func newFakeYouTube(t *testing.T) (*fakeYouTube, *httptest.Server) {
	fake := &fakeYouTube{t: t}
	server := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(server.Close)
	return fake, server
}

func (fake *fakeYouTube) serve(response http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.requestCount++
	require.Equal(fake.t, "Bearer youtube-token", request.Header.Get("Authorization"))
	fake.lastQuery = map[string]string{}
	for key, values := range request.URL.Query() {
		fake.lastQuery[key] = values[0]
	}
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Goog-Request-Id", "google-request")
	if fake.errorStatus != 0 {
		response.WriteHeader(fake.errorStatus)
		_, _ = fmt.Fprintf(response, `{"error":{"code":%d,"message":"secret detail","errors":[{"reason":%q}]}}`, fake.errorStatus, fake.errorReason)
		return
	}
	query := request.URL.Query()
	write := func(value any) { require.NoError(fake.t, json.NewEncoder(response).Encode(value)) }
	switch request.Method + " " + request.URL.Path {
	case "GET /search":
		write(map[string]any{"nextPageToken": "CAUQAA", "pageInfo": map[string]any{"totalResults": 1}, "items": []any{map[string]any{
			"id":      map[string]any{"kind": "youtube#video", "videoId": testVideoID},
			"snippet": map[string]any{"channelId": testChannelID, "channelTitle": "Google for Developers", "title": "Launch &amp; recap", "description": "d", "publishedAt": "2026-10-01T16:00:00Z"},
		}}})
	case "GET /videos":
		if query.Get("id") != testVideoID {
			write(map[string]any{"items": []any{}})
			return
		}
		write(map[string]any{"items": []any{map[string]any{
			"id":             testVideoID,
			"snippet":        map[string]any{"channelId": testChannelID, "channelTitle": "Google for Developers", "title": "Launch", "description": "d", "publishedAt": "2026-10-01T16:00:00Z"},
			"contentDetails": map[string]any{"duration": "PT4M13S"},
			"status":         map[string]any{"privacyStatus": "public"},
			"statistics":     map[string]any{"viewCount": "1200", "commentCount": "7"},
		}}})
	case "GET /channels":
		if query.Get("forHandle") == "@missing" {
			write(map[string]any{"kind": "youtube#channelListResponse"})
			return
		}
		write(map[string]any{"items": []any{map[string]any{
			"id":             testChannelID,
			"snippet":        map[string]any{"title": "Google for Developers", "customUrl": "@googledevelopers", "publishedAt": "2007-08-23T00:34:43Z"},
			"contentDetails": map[string]any{"relatedPlaylists": map[string]any{"uploads": "UU_x5XG1OV2P6uZZ5FSM9Ttw"}},
			"statistics":     map[string]any{"subscriberCount": "2500000", "hiddenSubscriberCount": false, "videoCount": "6000", "viewCount": "300000000"},
		}}})
	case "GET /playlistItems":
		if query.Get("playlistId") != testPlaylistID {
			response.WriteHeader(http.StatusNotFound)
			_, _ = response.Write([]byte(`{"error":{"errors":[{"reason":"playlistNotFound"}]}}`))
			return
		}
		matches := []any{}
		for _, item := range fake.items {
			if video := query.Get("videoId"); video == "" || item["snippet"].(map[string]any)["resourceId"].(map[string]any)["videoId"] == video {
				matches = append(matches, item)
			}
		}
		write(map[string]any{"pageInfo": map[string]any{"totalResults": len(matches)}, "items": matches})
	case "POST /playlistItems":
		var payload struct {
			Snippet struct {
				PlaylistID string `json:"playlistId"`
				ResourceID struct {
					Kind    string `json:"kind"`
					VideoID string `json:"videoId"`
				} `json:"resourceId"`
			} `json:"snippet"`
		}
		require.NoError(fake.t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(fake.t, "youtube#video", payload.Snippet.ResourceID.Kind)
		fake.inserts++
		item := map[string]any{
			"id": fmt.Sprintf("UExpdGVt%d", len(fake.items)),
			"snippet": map[string]any{
				"playlistId": payload.Snippet.PlaylistID, "title": "Launch", "position": len(fake.items), "publishedAt": "2026-10-13T17:00:00Z",
				"resourceId": map[string]any{"kind": "youtube#video", "videoId": payload.Snippet.ResourceID.VideoID},
			},
			"contentDetails": map[string]any{"videoPublishedAt": "2026-10-01T16:00:00Z"},
		}
		fake.items = append(fake.items, item)
		if fake.failInsert != 0 {
			status := fake.failInsert
			fake.failInsert = 0
			response.WriteHeader(status)
			return
		}
		write(item)
	default:
		response.WriteHeader(http.StatusNotFound)
	}
}

func newYouTubeClient(t *testing.T, endpoint string, maxBytes int64) *youtube.Client {
	t.Helper()
	return newYouTubeClientWithMethod(t, endpoint, maxBytes, youtube.ReadOnlyAuthMethodID)
}

func newYouTubeClientWithMethod(t *testing.T, endpoint string, maxBytes int64, method string) *youtube.Client {
	t.Helper()
	client, err := youtube.New(youtube.Config{Endpoint: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[youtube.Credentials]{
		youtubeConnection: {AuthMethodID: method, AccessToken: sdkgo.NewSecretString("youtube-token")},
	})
	require.NoError(t, err)
	return client
}

func TestSearchVideosSendsFiltersAndDecodesPage(t *testing.T) {
	fake, server := newFakeYouTube(t)
	client := newYouTubeClient(t, server.URL, 0)
	after := time.Date(2026, 9, 1, 0, 0, 0, 0, time.FixedZone("PDT", -7*3600))
	result, err := sdkgo.RunQuery(newDexContext("search"), client.SearchVideos(), youtubeConnection, youtube.SearchVideosInput{
		Query: "launch", ChannelID: testChannelID, Order: "date", PublishedAfter: &after, MaxResults: 10,
	})
	require.NoError(t, err)
	require.Equal(t, youtube.SearchVideosBranchSearched, result.Branch)
	require.Len(t, result.Value.Results, 1)
	require.Equal(t, testVideoID, result.Value.Results[0].VideoID)
	require.Equal(t, "CAUQAA", result.Value.NextPageToken)
	require.Equal(t, "google-request", result.Receipt.ProviderRequestID)
	require.Equal(t, map[string]string{
		"part": "snippet", "type": "video", "q": "launch", "channelId": testChannelID, "order": "date",
		"publishedAfter": "2026-09-01T07:00:00Z", "maxResults": "10",
	}, fake.lastQuery)
}

func TestSearchVideosRejectsInvalidInputWithoutCallingYouTube(t *testing.T) {
	fake, server := newFakeYouTube(t)
	client := newYouTubeClient(t, server.URL, 0)
	for name, input := range map[string]youtube.SearchVideosInput{
		"empty":     {},
		"order":     {Query: "x", Order: "newest"},
		"channel":   {ChannelID: "not-a-channel"},
		"page size": {Query: "x", MaxResults: 51},
		"token":     {Query: "x", PageToken: "a/b"},
	} {
		result, err := sdkgo.RunQuery(newDexContext("search-"+name), client.SearchVideos(), youtubeConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, youtube.SearchVideosBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
	require.Zero(t, fake.requestCount)
}

func TestGetVideoFoundAndNotFound(t *testing.T) {
	_, server := newFakeYouTube(t)
	client := newYouTubeClient(t, server.URL, 0)
	found, err := sdkgo.RunQuery(newDexContext("video"), client.GetVideo(), youtubeConnection, youtube.GetVideoInput{VideoID: testVideoID})
	require.NoError(t, err)
	require.Equal(t, youtube.GetVideoBranchFound, found.Branch)
	require.Equal(t, "PT4M13S", found.Value.Duration)
	require.Equal(t, int64(1200), *found.Value.ViewCount)
	require.Nil(t, found.Value.LikeCount)

	missing, err := sdkgo.RunQuery(newDexContext("video-missing"), client.GetVideo(), youtubeConnection, youtube.GetVideoInput{VideoID: "aaaaaaaaaaa"})
	require.NoError(t, err)
	require.Equal(t, youtube.GetVideoBranchNotFound, missing.Branch)
}

func TestGetChannelSelectorsAndNotFound(t *testing.T) {
	fake, server := newFakeYouTube(t)
	client := newYouTubeClient(t, server.URL, 0)
	found, err := sdkgo.RunQuery(newDexContext("channel"), client.GetChannel(), youtubeConnection, youtube.GetChannelInput{Handle: "googledevelopers"})
	require.NoError(t, err)
	require.Equal(t, youtube.GetChannelBranchFound, found.Branch)
	require.Equal(t, "@googledevelopers", fake.lastQuery["forHandle"])
	require.Equal(t, "UU_x5XG1OV2P6uZZ5FSM9Ttw", found.Value.UploadsPlaylistID)
	require.Equal(t, int64(2500000), *found.Value.SubscriberCount)

	missing, err := sdkgo.RunQuery(newDexContext("channel-missing"), client.GetChannel(), youtubeConnection, youtube.GetChannelInput{Handle: "@missing"})
	require.NoError(t, err)
	require.Equal(t, youtube.GetChannelBranchNotFound, missing.Branch)

	both, err := sdkgo.RunQuery(newDexContext("channel-both"), client.GetChannel(), youtubeConnection, youtube.GetChannelInput{ChannelID: testChannelID, Mine: true})
	require.NoError(t, err)
	require.Equal(t, youtube.GetChannelBranchDefect, both.Branch)
}

func TestListPlaylistItemsNotFoundPlaylist(t *testing.T) {
	_, server := newFakeYouTube(t)
	client := newYouTubeClient(t, server.URL, 0)
	result, err := sdkgo.RunQuery(newDexContext("list-missing"), client.ListPlaylistItems(), youtubeConnection, youtube.ListPlaylistItemsInput{PlaylistID: "PLmissing00"})
	require.NoError(t, err)
	require.Equal(t, youtube.ListPlaylistItemsBranchNotFound, result.Branch)
}

func TestAddPlaylistItemIsIdempotentAcrossAmbiguousInsert(t *testing.T) {
	fake, server := newFakeYouTube(t)
	client := newYouTubeClientWithMethod(t, server.URL, 0, youtube.ManageAuthMethodID)
	fake.failInsert = http.StatusServiceUnavailable
	ctx := newDexContext("add")
	input := youtube.AddPlaylistItemInput{PlaylistID: testPlaylistID, VideoID: testVideoID}

	_, err := sdkgo.RunMutation(ctx, client.AddPlaylistItem(), youtubeConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)

	recovered, err := sdkgo.RunMutation(ctx, client.AddPlaylistItem(), youtubeConnection, input)
	require.NoError(t, err)
	require.Equal(t, youtube.AddPlaylistItemBranchAdded, recovered.Branch)
	require.True(t, recovered.Value.WasAlreadyPresent)
	require.Equal(t, 1, fake.inserts)
	require.Len(t, fake.items, 1)
}

func TestAddPlaylistItemInsertsWhenAbsent(t *testing.T) {
	fake, server := newFakeYouTube(t)
	client := newYouTubeClientWithMethod(t, server.URL, 0, youtube.ManageAuthMethodID)
	result, err := sdkgo.RunMutation(newDexContext("add-new"), client.AddPlaylistItem(), youtubeConnection, youtube.AddPlaylistItemInput{PlaylistID: testPlaylistID, VideoID: testVideoID})
	require.NoError(t, err)
	require.Equal(t, youtube.AddPlaylistItemBranchAdded, result.Branch)
	require.False(t, result.Value.WasAlreadyPresent)
	require.Equal(t, testVideoID, result.Value.Item.VideoID)
	require.Equal(t, result.Value.Item.ID, result.Receipt.ProviderObjectID)
	require.Equal(t, 1, fake.inserts)
}

func TestAddPlaylistItemOnReadOnlyConnectionRejectsWithoutCallingYouTube(t *testing.T) {
	for _, method := range []string{youtube.ReadOnlyAuthMethodID, ""} {
		fake, server := newFakeYouTube(t)
		client := newYouTubeClientWithMethod(t, server.URL, 0, method)
		result, err := sdkgo.RunMutation(newDexContext("add-readonly"+method), client.AddPlaylistItem(), youtubeConnection, youtube.AddPlaylistItemInput{PlaylistID: testPlaylistID, VideoID: testVideoID})
		require.NoError(t, err)
		require.Equal(t, youtube.AddPlaylistItemBranchProviderRejected, result.Branch)
		require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
		require.Zero(t, fake.requestCount)
	}
}

func TestErrorClassification(t *testing.T) {
	tests := []struct {
		status    int
		reason    string
		branch    sdkgo.BranchID
		kind      sdkgo.FailureKind
		wantRetry bool
	}{
		{http.StatusForbidden, "quotaExceeded", youtube.GetVideoBranchQuotaExhausted, sdkgo.FailureQuotaExhausted, false},
		{http.StatusForbidden, "forbidden", youtube.GetVideoBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{http.StatusForbidden, "rateLimitExceeded", "", sdkgo.FailureRateLimit, true},
		{http.StatusTooManyRequests, "", "", sdkgo.FailureRateLimit, true},
		{http.StatusInternalServerError, "", "", sdkgo.FailureAvailability, true},
		{http.StatusBadRequest, "invalidParameter", youtube.GetVideoBranchProviderRejected, sdkgo.FailureValidation, false},
		{http.StatusNotFound, "", youtube.GetVideoBranchNotFound, sdkgo.FailureNotFound, false},
	}
	for _, test := range tests {
		name := fmt.Sprintf("%d-%s", test.status, test.reason)
		t.Run(name, func(t *testing.T) {
			fake, server := newFakeYouTube(t)
			fake.errorStatus, fake.errorReason = test.status, test.reason
			client := newYouTubeClient(t, server.URL, 0)
			result, err := sdkgo.RunQuery(newDexContext(name), client.GetVideo(), youtubeConnection, youtube.GetVideoInput{VideoID: testVideoID})
			if test.wantRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, test.kind, retry.Failure.Kind)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "secret detail")
		})
	}
}

func TestOversizedAndMalformedResponsesUseInvalidResponse(t *testing.T) {
	_, server := newFakeYouTube(t)
	small := newYouTubeClient(t, server.URL, 16)
	result, err := sdkgo.RunQuery(newDexContext("oversized"), small.GetVideo(), youtubeConnection, youtube.GetVideoInput{VideoID: testVideoID})
	require.NoError(t, err)
	require.Equal(t, youtube.GetVideoBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)

	malformed := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"items":`))
	}))
	t.Cleanup(malformed.Close)
	client := newYouTubeClient(t, malformed.URL, 0)
	result, err = sdkgo.RunQuery(newDexContext("malformed"), client.GetVideo(), youtubeConnection, youtube.GetVideoInput{VideoID: testVideoID})
	require.NoError(t, err)
	require.Equal(t, youtube.GetVideoBranchInvalidResponse, result.Branch)
}

type rejectionRefreshingCredentialProvider struct{ forcedRefreshes int }

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (youtube.Credentials, error) {
	return youtube.Credentials{AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

func (provider *rejectionRefreshingCredentialProvider) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[youtube.Credentials]) (youtube.Credentials, error) {
	return provider.Resolve(call)
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[youtube.Credentials]) (youtube.Credentials, error) {
	provider.forcedRefreshes++
	return youtube.Credentials{AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

func TestRefreshesOnceAfterUnauthorized(t *testing.T) {
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		tokens = append(tokens, strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "))
		response.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	provider := &rejectionRefreshingCredentialProvider{}
	client, err := youtube.New(youtube.Config{Endpoint: server.URL}, provider)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newDexContext("refresh"), client.GetVideo(), youtubeConnection, youtube.GetVideoInput{VideoID: testVideoID})
	require.NoError(t, err)
	require.Equal(t, youtube.GetVideoBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, []string{"rejected-token", "replacement-token"}, tokens)
	require.Equal(t, 1, provider.forcedRefreshes)
}

func TestMissingCredentialUsesDefect(t *testing.T) {
	client, err := youtube.New(youtube.Config{}, sdkgo.StaticCredentialProvider[youtube.Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newDexContext("missing"), client.GetVideo(), youtubeConnection, youtube.GetVideoInput{VideoID: testVideoID})
	require.NoError(t, err)
	require.Equal(t, youtube.GetVideoBranchDefect, result.Branch)
}

func TestNewRejectsInsecureEndpoint(t *testing.T) {
	_, err := youtube.New(youtube.Config{Endpoint: "http://example.com"}, sdkgo.StaticCredentialProvider[youtube.Credentials]{})
	require.Error(t, err)
}

type dexContext struct {
	context.Context
	step string
}

func newDexContext(step string) *dexContext {
	return &dexContext{Context: context.Background(), step: step}
}
func (*dexContext) FlowID() string                                  { return "youtube-flow" }
func (*dexContext) RunID() string                                   { return "run" }
func (*dexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *dexContext) StepExecutionID() string                 { return context.step }
func (*dexContext) FromStepExecutionID() string                     { return "" }
func (*dexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*dexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*dexContext) Attempt() int32                                  { return 1 }
func (*dexContext) HasTimerFired() bool                             { return false }
func (*dexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*dexContext) WaitForMethodFailed() bool                       { return false }
func (*dexContext) RecordHeartbeat(any) error                       { return nil }
func (*dexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*dexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*dexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*dexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*dexContext)(nil)
