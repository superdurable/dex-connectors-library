// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "telegram-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "telegram", Name: "telegram"}

const testBotToken = "123456:fake_test_token"

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: Credentials{BotToken: sdkgo.NewSecretString(testBotToken)}})
	require.NoError(t, err)
	return client
}

func TestGetChatProviderOutcomesAndSecretSafety(t *testing.T) {
	status, body, requests := http.StatusOK, `{"ok":true,"result":{"id":-1001234567890,"type":"supergroup","title":"Meridian Team","username":"meridian_team"}}`, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/bot"+testBotToken+"/getChat", r.URL.Path)
		require.Equal(t, "@meridian_team", r.URL.Query().Get("chat_id"))
		w.WriteHeader(status)
		_, err := fmt.Fprint(w, body)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	input := GetChatInput{ChatID: "@meridian_team"}
	found, err := sdkgo.RunQuery(queryContext{}, client.GetChat(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetChatBranchFound, found.Branch)
	require.Equal(t, "Meridian Team", found.Value.Title)
	status, body = http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`
	missing, err := sdkgo.RunQuery(queryContext{}, client.GetChat(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetChatBranchNotFound, missing.Branch)
	status, body = http.StatusUnauthorized, `{"ok":false,"error_code":401,"description":"Unauthorized"}`
	rejected, err := sdkgo.RunQuery(queryContext{}, client.GetChat(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetChatBranchProviderRejected, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, testBotToken)
	status, body = http.StatusOK, `{}`
	invalid, err := sdkgo.RunQuery(queryContext{}, client.GetChat(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetChatBranchInvalidResponse, invalid.Branch)
	status, body = http.StatusTooManyRequests, `{"ok":false,"error_code":429}`
	_, err = sdkgo.RunQuery(queryContext{}, client.GetChat(), testConnection, input)
	require.Error(t, err)
	require.NotContains(t, err.Error(), testBotToken)
	status, body = http.StatusServiceUnavailable, `{"ok":false,"error_code":503}`
	_, err = sdkgo.RunQuery(queryContext{}, client.GetChat(), testConnection, input)
	require.Error(t, err)
	badInput, err := sdkgo.RunQuery(queryContext{}, client.GetChat(), testConnection, GetChatInput{ChatID: "bad value"})
	require.NoError(t, err)
	require.Equal(t, GetChatBranchDefect, badInput.Branch)
	require.Equal(t, 6, requests)
}
