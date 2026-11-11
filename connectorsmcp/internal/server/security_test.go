// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
)

func TestHostAndOriginMatrix(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{allowedHosts: []string{"Proxy.Test"}})
	protected := harness.server.Handler()
	cases := []struct {
		name, host, origin, fetchSite string
		expectedStatus                int
	}{
		{"loopback IPv4", "127.0.0.1:8803", "", "", http.StatusOK},
		{"loopback IPv4 range", "127.0.0.2:8803", "", "", http.StatusOK},
		{"loopback IPv6", "[::1]:8803", "", "", http.StatusOK},
		{"localhost", "localhost:8803", "", "", http.StatusOK},
		{"localhost subdomain", "dev.localhost:8803", "", "", http.StatusOK},
		{"localhost with trailing dot", "LOCALHOST.:8803", "", "", http.StatusOK},
		{"allowed host", "proxy.test", "", "", http.StatusOK},
		{"DNS rebinding name", "attacker.example:8803", "", "", http.StatusForbidden},
		{"LAN IP literal", "192.168.1.5:8803", "", "", http.StatusForbidden},
		{"unspecified IP literal", "0.0.0.0:8803", "", "", http.StatusForbidden},
		{"empty Host", "", "", "", http.StatusForbidden},
		{"same-origin Origin", "127.0.0.1:8803", "http://127.0.0.1:8803", "", http.StatusOK},
		{"cross-site Origin", "127.0.0.1:8803", "http://evil.example", "", http.StatusForbidden},
		{"https Origin", "localhost:8803", "https://localhost:8803", "", http.StatusForbidden},
		{"null Origin", "localhost:8803", "null", "", http.StatusForbidden},
		{"Origin with a path", "localhost:8803", "http://localhost:8803/x", "", http.StatusForbidden},
		{"cross-site fetch metadata", "127.0.0.1:8803", "", "cross-site", http.StatusForbidden},
	}
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`)
	for _, testCase := range cases {
		request := httptest.NewRequest(http.MethodPost, MCPPath, bytes.NewReader(body))
		request.Host = testCase.host
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("MCP-Protocol-Version", testProtocolVersion)
		request.Header.Set("Mcp-Method", "tools/list")
		if testCase.origin != "" {
			request.Header.Set("Origin", testCase.origin)
		}
		if testCase.fetchSite != "" {
			request.Header.Set("Sec-Fetch-Site", testCase.fetchSite)
		}
		recorder := httptest.NewRecorder()
		protected.ServeHTTP(recorder, request)
		require.Equal(t, testCase.expectedStatus, recorder.Code, testCase.name)
		require.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"), testCase.name)
	}
}

func TestOversizedBodiesAreRefused(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	huge := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"padding":"` + strings.Repeat("x", maximumRequestBodyBytes) + `"}}`
	request, err := http.NewRequest(http.MethodPost, harness.httpServer.URL+MCPPath, strings.NewReader(huge))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode)
}

func TestListenAddressMustBeLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8803", "127.0.0.1:0", "[::1]:8803", "localhost:0", "127.1.2.3:9"} {
		require.NoError(t, ValidateListenAddress(address), address)
	}
	for _, address := range []string{"0.0.0.0:8803", "[::]:8803", ":8803", "192.168.1.5:8803", "example.com:8803", "127.0.0.1", "localhost"} {
		require.Error(t, ValidateListenAddress(address), address)
	}
}

func TestHealthReportsIdentityWithoutToolNames(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	response, err := http.Get(harness.httpServer.URL + HealthPath)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	contents, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	var health map[string]any
	require.NoError(t, json.Unmarshal(contents, &health))
	require.Equal(t, map[string]any{"status": "ok", "version": "0.1.0-test", "launchContract": float64(1), "instanceId": "instance-test", "toolCount": float64(3)}, health)
	require.NotContains(t, string(contents), "example-notes")
	require.NotContains(t, string(contents), "work")

	request := httptest.NewRequest(http.MethodGet, HealthPath, nil)
	request.Host = "attacker.example"
	recorder := httptest.NewRecorder()
	harness.server.Handler().ServeHTTP(recorder, request)
	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestLogsNeverContainKeysArgumentsResultsOrCredentials(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	harness.mustCallResult("example-notes_post_note", map[string]any{"text": "secret-argument-text"}, "flow/turn/secret-key-value")
	harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "n-1"}, "")
	harness.provider.Script("postNote", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	harness.mustCallResult("example-notes_post_note", map[string]any{"text": "secret-argument-text"}, "flow/turn/secret-key-2")
	logs := harness.logs.String()
	require.Contains(t, logs, `"msg":"tool call"`)
	for _, secret := range []string{"secret-argument-text", "secret-key-value", "secret-key-2", testconnectors.ProviderToken, "n-1"} {
		require.NotContains(t, logs, secret)
	}
}
