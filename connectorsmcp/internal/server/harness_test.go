// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/callstate"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
)

const testProtocolVersion = "2026-07-28"

// fakeClock advances by the requested duration on every Sleep.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
	// oversleep is added to every Sleep, as scheduling delay would.
	oversleep time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)}
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) Sleep(ctx context.Context, duration time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration + clock.oversleep)
	clock.sleeps = append(clock.sleeps, duration)
	return true
}

func (clock *fakeClock) setOversleep(oversleep time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.oversleep = oversleep
}

func (clock *fakeClock) advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

func (clock *fakeClock) recordedSleeps() []time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return append([]time.Duration(nil), clock.sleeps...)
}

// synchronizedBuffer collects log output from concurrent handlers.
type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *synchronizedBuffer) Write(contents []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(contents)
}

func (buffer *synchronizedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}

// testHarness serves the notes connector from a temporary connections file.
type testHarness struct {
	t               *testing.T
	provider        *testconnectors.FakeProvider
	connectionsFile string
	stateDirectory  string
	clock           *fakeClock
	logs            *synchronizedBuffer
	server          *Server
	httpServer      *httptest.Server
	toolSetChanges  []string
	chatSends       *testconnectors.ChatSends
	isChatLinked    bool
}

type harnessOptions struct {
	connectionNames []string
	selection       *ConnectionSelection
	callTimeBudget  time.Duration
	maxResultBytes  int
	extraConnectors []registry.LinkedConnector
	allowedHosts    []string
	observe         func(ProtocolPoint, string)
	// stateDirectory shares another harness's call state; empty uses a new one.
	stateDirectory string
	// isChatLinked links the chat connector and authorizes example-chat/work.
	isChatLinked bool
}

func newTestHarness(t *testing.T, options harnessOptions) *testHarness {
	t.Helper()
	provider := testconnectors.NewFakeProvider()
	t.Cleanup(provider.Close)
	directory := t.TempDir()
	harness := &testHarness{
		t: t, provider: provider, connectionsFile: filepath.Join(directory, "connectors", "connections.json"),
		stateDirectory: filepath.Join(directory, "state"), clock: newFakeClock(), logs: &synchronizedBuffer{},
		chatSends: &testconnectors.ChatSends{}, isChatLinked: options.isChatLinked,
	}
	if options.stateDirectory != "" {
		harness.stateDirectory = options.stateDirectory
	}
	if options.isChatLinked {
		options.extraConnectors = append(options.extraConnectors, testconnectors.ChatConnector(harness.chatSends))
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(harness.connectionsFile), 0o700))
	if options.connectionNames == nil {
		options.connectionNames = []string{"work"}
	}
	harness.writeConnections(options.connectionNames...)
	if options.callTimeBudget == 0 {
		options.callTimeBudget = 5 * time.Minute
	}
	if options.maxResultBytes == 0 {
		options.maxResultBytes = 65536
	}
	linked, err := registry.New(append([]registry.LinkedConnector{testconnectors.NotesConnector()}, options.extraConnectors...))
	require.NoError(t, err)
	store, err := callstate.Open(harness.stateDirectory, harness.clock.Now)
	require.NoError(t, err)
	logger := slog.New(slog.NewJSONHandler(harness.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	harness.server, err = New(context.Background(), &Config{
		Version: "0.1.0-test", InstanceID: "instance-test",
		ToolSet: ToolSetConfig{Registry: linked, ConnectionsFile: harness.connectionsFile, Selection: options.selection, Logger: logger},
		Store:   store, AllowedHosts: options.allowedHosts, CallTimeBudget: options.callTimeBudget, CallStateRetention: 720 * time.Hour,
		MaxResultBytes: options.maxResultBytes, Clock: harness.clock, ObserveProtocolPoint: options.observe,
		OnToolSetChange: func(toolSet *ToolSet, _ *string) {
			harness.toolSetChanges = append(harness.toolSetChanges, toolSet.Digest())
		},
	})
	require.NoError(t, err)
	harness.httpServer = httptest.NewServer(harness.server.Handler())
	t.Cleanup(harness.httpServer.Close)
	return harness
}

func (harness *testHarness) writeConnections(names ...string) {
	harness.t.Helper()
	connections := make([]testconnectors.ConnectionsFileConnection, 0, len(names))
	for _, name := range names {
		connections = append(connections, testconnectors.NotesConnection(name, harness.provider))
	}
	if harness.isChatLinked {
		connections = append(connections, testconnectors.ChatConnection("work"))
	}
	require.NoError(harness.t, testconnectors.WriteConnectionsFile(harness.connectionsFile, connections...))
}

// rpcResponse is a decoded JSON-RPC response.
type rpcResponse struct {
	HTTPStatus int
	Result     json.RawMessage `json:"result"`
	Error      *struct {
		Code    int64           `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

// callResult is a decoded tools/call result.
type callResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	Meta              map[string]any  `json:"_meta"`
}

func (result callResult) text() string {
	if len(result.Content) == 0 {
		return ""
	}
	return result.Content[0].Text
}

func (result callResult) outcome() any { return result.Meta[metaKeyOutcome] }

// callTool calls a tool over HTTP with MCP 2026-07-28; an empty key sends none.
func (harness *testHarness) callTool(name string, arguments any, key string) rpcResponse {
	harness.t.Helper()
	return harness.postRPC(toolCallBody(name, arguments, key), nil)
}

func (harness *testHarness) mustCallResult(name string, arguments any, key string) callResult {
	harness.t.Helper()
	response := harness.callTool(name, arguments, key)
	require.Nil(harness.t, response.Error, "unexpected JSON-RPC error")
	var result callResult
	require.NoError(harness.t, json.Unmarshal(response.Result, &result))
	return result
}

func (harness *testHarness) listTools() []map[string]any {
	harness.t.Helper()
	response := harness.postRPC(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{
		"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": testProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}},
	}}, nil)
	require.Nil(harness.t, response.Error)
	var listed struct {
		Tools []map[string]any `json:"tools"`
	}
	require.NoError(harness.t, json.Unmarshal(response.Result, &listed))
	return listed.Tools
}

func (harness *testHarness) postRPC(body any, headers map[string]string) rpcResponse {
	harness.t.Helper()
	response, err := postJSONRPC(harness.httpServer.URL+MCPPath, body, headers)
	require.NoError(harness.t, err)
	return response
}

// postJSONRPC posts one MCP 2026-07-28 request and decodes its answer.
func postJSONRPC(endpoint string, body any, headers map[string]string) (rpcResponse, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return rpcResponse{}, err
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return rpcResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", testProtocolVersion)
	if message, isMessage := body.(map[string]any); isMessage {
		method, _ := message["method"].(string)
		request.Header.Set("Mcp-Method", method)
		if params, hasParams := message["params"].(map[string]any); hasParams && method == "tools/call" {
			if name, hasName := params["name"].(string); hasName {
				request.Header.Set("Mcp-Name", name)
			}
		}
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return rpcResponse{}, err
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		return rpcResponse{}, err
	}
	decoded := rpcResponse{HTTPStatus: response.StatusCode}
	// MCP 2026-07-28 answers -32602 with HTTP 400 and a JSON-RPC body.
	if response.Header.Get("Content-Type") == "application/json" {
		if err := json.Unmarshal(contents, &decoded); err != nil {
			return rpcResponse{}, fmt.Errorf("decode %s: %w", contents, err)
		}
	}
	return decoded, nil
}

// toolCallBody is a tools/call request; an empty key sends none.
func toolCallBody(name string, arguments any, key string) map[string]any {
	meta := map[string]any{"io.modelcontextprotocol/protocolVersion": testProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
	if key != "" {
		meta[metaKeyIdempotencyKey] = key
	}
	params := map[string]any{"name": name, "_meta": meta}
	if arguments != nil {
		params["arguments"] = arguments
	}
	return map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": params}
}
