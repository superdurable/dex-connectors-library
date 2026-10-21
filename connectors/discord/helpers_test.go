// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testBotToken  = "MTAxTESTbot.token-part.0123456789abcdef"
	testChannelID = "112233445566778899"
	testMessageID = "998877665544332211"
	testAuthorID  = "123456789012345678"
	testGuildID   = "223344556677889900"
	// providerMessageSentinel appears only in fake provider message text, which must never reach a Failure.
	providerMessageSentinel = "SENTINEL provider message text"
)

var discordConnection = sdkgo.ConnectionRef{Provider: "discord", Name: "discord"}

// recordingDiscord is a fake Discord API that records every request; each test supplies its handler.
type recordingDiscord struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	method        string
	path          string
	rawPath       string
	query         string
	authorization string
	userAgent     string
	contentType   string
	body          []byte
}

func newRecordingDiscord(t *testing.T, handler func(http.ResponseWriter, recordedRequest)) *recordingDiscord {
	t.Helper()
	provider := &recordingDiscord{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read fake Discord request body: %v", err)
		}
		recorded := recordedRequest{
			method: request.Method, path: request.URL.Path, rawPath: request.URL.EscapedPath(), query: request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"), userAgent: request.Header.Get("User-Agent"), contentType: request.Header.Get("Content-Type"), body: body,
		}
		provider.mutex.Lock()
		provider.requests = append(provider.requests, recorded)
		provider.mutex.Unlock()
		handler(response, recorded)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingDiscord) recordedRequests() []recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]recordedRequest(nil), provider.requests...)
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		t.Errorf("write fake Discord response: %v", err)
	}
}

func newTestClient(t *testing.T, endpoint string, config discord.Config, options ...discord.Option) *discord.Client {
	t.Helper()
	config.Endpoint = endpoint
	client, err := discord.New(config, sdkgo.StaticCredentialProvider[discord.Credentials]{
		discordConnection: {BotToken: sdkgo.NewSecretString(testBotToken)},
	}, options...)
	require.NoError(t, err)
	return client
}

// requireRetry asserts that a connector call asked Dex to retry, and returns the requested delay and Failure.
func requireRetry(t *testing.T, err error) (time.Duration, sdkgo.Failure) {
	t.Helper()
	require.Error(t, err)
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	delay := time.Duration(0)
	var retryAfter *dex.RetryAfterError
	if errors.As(err, &retryAfter) {
		delay = retryAfter.After
	}
	return delay, retryError.Failure
}

// requireSecretSafeFailure asserts a Failure names neither the token nor provider message text.
func requireSecretSafeFailure(t *testing.T, failure *sdkgo.Failure) {
	t.Helper()
	require.NotNil(t, failure)
	require.NotContains(t, failure.Message, testBotToken)
	require.NotContains(t, failure.Message, providerMessageSentinel)
}

type stepDexContext struct {
	context.Context
	step string
}

func newStepDexContext(step string) *stepDexContext {
	return &stepDexContext{Context: context.Background(), step: step}
}

func (*stepDexContext) FlowID() string                                  { return "announcement-flow" }
func (*stepDexContext) RunID() string                                   { return "run" }
func (*stepDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (ctx *stepDexContext) StepExecutionID() string                     { return ctx.step }
func (*stepDexContext) FromStepExecutionID() string                     { return "" }
func (*stepDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*stepDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*stepDexContext) Attempt() int32                                  { return 1 }
func (*stepDexContext) HasTimerFired() bool                             { return false }
func (*stepDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*stepDexContext) WaitForMethodFailed() bool                       { return false }
func (*stepDexContext) RecordHeartbeat(any) error                       { return nil }
func (*stepDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*stepDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*stepDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*stepDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*stepDexContext)(nil)
