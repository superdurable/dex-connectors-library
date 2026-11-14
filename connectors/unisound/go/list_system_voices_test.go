// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package unisound

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "unisound", Name: "unisound"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "unisound-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-system-voices" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(token any) any         { return context.Background().Value(token) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListSystemVoicesRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, voiceQueryEndpoint, request.URL.String())
		require.Equal(t, "Bearer test-secret", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var payload map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, map[string]string{"voice_type": "system"}, payload)
		return reply(200, `{"system_voice":[{"voice_id":"cn_male_chenyu","voice_name":"晨宇","language":["zh"],"description":["private-description"]}],"voice_cloning":[{"voice_id":"private-clone"}],"base_resp":{"status_code":0,"status_msg":"success"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListSystemVoices(), testConnection, ListSystemVoicesInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListSystemVoicesBranchFound, result.Branch)
	require.Equal(t, ListSystemVoicesOutput{Voices: []SystemVoice{{VoiceID: "cn_male_chenyu", Name: "晨宇", Languages: []string{"zh"}}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-")
}
func TestListSystemVoicesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"system_voice":[],"base_resp":{"status_code":0}}`, ListSystemVoicesBranchFound, false},
		{"bad key", 401, `{"message":"test-secret"}`, ListSystemVoicesBranchProviderRejected, false},
		{"forbidden", 403, `{"message":"test-secret"}`, ListSystemVoicesBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true}, {"server", 503, `{}`, "", true},
		{"business reject", 200, `{"base_resp":{"status_code":100101,"status_msg":"test-secret"}}`, ListSystemVoicesBranchProviderRejected, false},
		{"business limit", 200, `{"base_resp":{"status_code":100501}}`, "", true},
		{"missing code", 200, `{"system_voice":[]}`, ListSystemVoicesBranchInvalidResponse, false},
		{"missing voices", 200, `{"base_resp":{"status_code":0}}`, ListSystemVoicesBranchInvalidResponse, false},
		{"bad voice", 200, `{"system_voice":[{"voice_id":"","language":[]}],"base_resp":{"status_code":0}}`, ListSystemVoicesBranchInvalidResponse, false},
		{"redirect", 302, `{}`, ListSystemVoicesBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListSystemVoices(), testConnection, ListSystemVoicesInput{})
			require.Equal(t, 1, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "test-secret")
			}
		})
	}
}
func TestListSystemVoicesOversizeAndTransport(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ListSystemVoices(), testConnection, ListSystemVoicesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSystemVoicesBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListSystemVoices(), testConnection, ListSystemVoicesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
