//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channelannouncement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationBotToken = "MTAxINTEGRATION.bot-token.0123456789abcdef"
	// healthyChannelID, flakyChannelID, and unknownChannelID select the fake's behavior.
	healthyChannelID = "100000000000000001"
	flakyChannelID   = "100000000000000002"
	unknownChannelID = "100000000000000404"
	botUserID        = "200000000000000001"
)

func TestChannelAnnouncementExampleRunsEveryDiscordOperationWithRealDex(t *testing.T) {
	provider := newDiscordProvider(t)
	flow, harness := newAnnouncementIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)

	t.Run("an announcement is posted, reacted to, and read back", func(t *testing.T) {
		flowID := "discord-announcement-healthy-" + testRunID
		text := "Release " + testRunID + " @everyone"
		result := harness.runFlow(t, ctx, flow, flowID, AnnouncementRequest{ChannelID: healthyChannelID, Text: text})
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var outcome AnnouncementOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.True(t, outcome.IsConfirmed)
		require.Equal(t, "announcements", outcome.ChannelName)
		require.Equal(t, "📣", outcome.ReactionEmoji)
		messages := provider.messagesIn(healthyChannelID)
		require.Len(t, messages, 1)
		require.Equal(t, text, messages[0].Content)
		require.Equal(t, outcome.MessageID, messages[0].ID)
		require.Equal(t, []string{"📣"}, provider.reactionsOn(outcome.MessageID))

		var display map[string]any
		require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.GetDexDisplay, nil, &display))
		encodedDisplay, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encodedDisplay), outcome.MessageID)
		require.NotContains(t, string(encodedDisplay), integrationBotToken)
	})

	t.Run("a post whose response was lost is retried with the same nonce and creates one message", func(t *testing.T) {
		result := harness.runFlow(t, ctx, flow, "discord-announcement-flaky-"+testRunID, AnnouncementRequest{ChannelID: flakyChannelID, Text: "Flaky " + testRunID})
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		require.Equal(t, 2, provider.postAttempts(flakyChannelID), "the first post was applied but answered 503")
		require.Len(t, provider.messagesIn(flakyChannelID), 1, "the enforced nonce returned the first message")
	})

	t.Run("an unknown channel fails the Flow on the unwired notFound branch before posting", func(t *testing.T) {
		result := harness.runFlow(t, ctx, flow, "discord-announcement-unknown-"+testRunID, AnnouncementRequest{ChannelID: unknownChannelID, Text: "hi"})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Zero(t, provider.postAttempts(unknownChannelID))
	})

	t.Run("invalid start input fails before calling Discord", func(t *testing.T) {
		before := provider.requestCount()
		result := harness.runFlow(t, ctx, flow, "discord-announcement-invalid-"+testRunID, AnnouncementRequest{ChannelID: "#general", Text: "hi"})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Equal(t, before, provider.requestCount())
	})
}

type fakeMessage struct {
	ID      string
	Content string
	Nonce   string
}

// discordProvider is a stateful local Discord API that enforces message nonces like Discord.
type discordProvider struct {
	*httptest.Server
	mutex         sync.Mutex
	requests      int
	nextMessageID int64
	messages      map[string][]fakeMessage
	posts         map[string]int
	reactions     map[string][]string
}

func newDiscordProvider(t *testing.T) *discordProvider {
	t.Helper()
	provider := &discordProvider{
		nextMessageID: 300000000000000000, messages: map[string][]fakeMessage{}, posts: map[string]int{}, reactions: map[string][]string{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *discordProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.requests++
	if request.Header.Get("Authorization") != "Bot "+integrationBotToken {
		writeProviderResponse(response, http.StatusUnauthorized, `{"message":"401: Unauthorized","code":0}`)
		return
	}
	segments := strings.Split(strings.TrimPrefix(request.URL.EscapedPath(), "/api/v10/channels/"), "/")
	channelID := segments[0]
	if channelID != healthyChannelID && channelID != flakyChannelID {
		writeProviderResponse(response, http.StatusNotFound, `{"message":"Unknown Channel","code":10003}`)
		return
	}
	switch {
	case request.Method == http.MethodGet && len(segments) == 1:
		writeProviderResponse(response, http.StatusOK, fmt.Sprintf(`{"id":%q,"type":0,"guild_id":"400000000000000001","name":"announcements"}`, channelID))
	case request.Method == http.MethodPost && len(segments) == 2:
		provider.createMessage(response, request, channelID)
	case request.Method == http.MethodPut && len(segments) == 6 && segments[3] == "reactions" && segments[5] == "@me":
		emoji, err := url.PathUnescape(segments[4])
		if err != nil {
			writeProviderResponse(response, http.StatusBadRequest, `{"code":50035}`)
			return
		}
		if !slices.Contains(provider.reactions[segments[2]], emoji) {
			provider.reactions[segments[2]] = append(provider.reactions[segments[2]], emoji)
		}
		response.WriteHeader(http.StatusNoContent)
	case request.Method == http.MethodGet && len(segments) == 2:
		messages := provider.messages[channelID]
		encoded := make([]string, 0, len(messages))
		for index := len(messages) - 1; index >= 0; index-- {
			encoded = append(encoded, messageResponse(channelID, messages[index]))
		}
		writeProviderResponse(response, http.StatusOK, "["+strings.Join(encoded, ",")+"]")
	default:
		writeProviderResponse(response, http.StatusNotFound, `{"message":"404: Not Found","code":0}`)
	}
}

func (provider *discordProvider) createMessage(response http.ResponseWriter, request *http.Request, channelID string) {
	provider.posts[channelID]++
	var body struct {
		Content      string `json:"content"`
		Nonce        string `json:"nonce"`
		EnforceNonce bool   `json:"enforce_nonce"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil || !body.EnforceNonce || body.Nonce == "" {
		writeProviderResponse(response, http.StatusBadRequest, `{"code":50035}`)
		return
	}
	for _, existing := range provider.messages[channelID] {
		if existing.Nonce == body.Nonce {
			writeProviderResponse(response, http.StatusOK, messageResponse(channelID, existing))
			return
		}
	}
	provider.nextMessageID++
	message := fakeMessage{ID: strconv.FormatInt(provider.nextMessageID, 10), Content: body.Content, Nonce: body.Nonce}
	provider.messages[channelID] = append(provider.messages[channelID], message)
	if channelID == flakyChannelID && provider.posts[channelID] == 1 {
		writeProviderResponse(response, http.StatusServiceUnavailable, `{"message":"upstream","code":0}`)
		return
	}
	writeProviderResponse(response, http.StatusOK, messageResponse(channelID, message))
}

func messageResponse(channelID string, message fakeMessage) string {
	content, _ := json.Marshal(message.Content)
	return fmt.Sprintf(`{"id":%q,"channel_id":%q,"type":0,"content":%s,"nonce":%q,"timestamp":"2026-10-13T20:00:00+00:00",`+
		`"edited_timestamp":null,"author":{"id":%q,"username":"dex-bot","bot":true}}`, message.ID, channelID, content, message.Nonce, botUserID)
}

func writeProviderResponse(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		panic(err)
	}
}

func (provider *discordProvider) messagesIn(channelID string) []fakeMessage {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]fakeMessage(nil), provider.messages[channelID]...)
}

func (provider *discordProvider) reactionsOn(messageID string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.reactions[messageID]...)
}

func (provider *discordProvider) postAttempts(channelID string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.posts[channelID]
}

func (provider *discordProvider) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests
}

type announcementIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newAnnouncementIntegrationHarness(t *testing.T, providerURL string) (*Flow, *announcementIntegrationHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "discord", Name: ConnectionName}
	providerClient, err := discord.New(discord.Config{Endpoint: providerURL + "/api/v10"}, sdkgo.StaticCredentialProvider[discord.Credentials]{
		reference: {BotToken: sdkgo.NewSecretString(integrationBotToken)},
	})
	require.NoError(t, err)
	connection, err := discord.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &announcementIntegrationHarness{
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

func (harness *announcementIntegrationHarness) startWorker(t *testing.T) {
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

func (harness *announcementIntegrationHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func (harness *announcementIntegrationHarness) runFlow(t *testing.T, ctx context.Context, flow *Flow, flowID string, input AnnouncementRequest) dex.FlowResult {
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
