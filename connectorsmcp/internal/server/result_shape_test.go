// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
)

// requireCallToolResultShape checks a tools/call result against the MCP
// CallToolResult schema; MCP 2026-07-28 requires resultType.
func requireCallToolResultShape(t *testing.T, name string, raw json.RawMessage, isResultTypeExpected bool) {
	t.Helper()
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields), name)
	allowed := map[string]bool{"_meta": true, "content": true, "structuredContent": true, "isError": true, "resultType": true}
	for field := range fields {
		require.True(t, allowed[field], "%s: unexpected field %s", name, field)
	}
	var content []map[string]any
	require.NoError(t, json.Unmarshal(fields["content"], &content), "%s: content must be an array", name)
	require.NotNil(t, content, "%s: content is required", name)
	for _, block := range content {
		require.Equal(t, "text", block["type"], name)
		require.IsType(t, "", block["text"], name)
	}
	if structured, isPresent := fields["structuredContent"]; isPresent {
		require.True(t, strings.HasPrefix(string(structured), "{"), "%s: structuredContent must be an object", name)
	}
	if isError, isPresent := fields["isError"]; isPresent {
		require.Contains(t, []string{"true", "false"}, string(isError), name)
	}
	resultType, hasResultType := fields["resultType"]
	if isResultTypeExpected {
		require.True(t, hasResultType, "%s: CallToolResult.resultType is required under MCP 2026-07-28", name)
		require.JSONEq(t, `"complete"`, string(resultType), name)
	} else {
		require.False(t, hasResultType, "%s: resultType is not part of older protocol versions", name)
	}
}

func TestToolsCallResultsCarryResultTypeUnderMCP20260728(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{maxResultBytes: 1024})
	cases := []struct {
		name  string
		call  func() rpcResponse
		check func(callResult) bool
	}{
		{"success with structured content", func() rpcResponse {
			return harness.callTool("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
		}, func(result callResult) bool { return !result.IsError && result.StructuredContent != nil }},
		{"failed read", func() rpcResponse {
			harness.provider.Script("getNote", testconnectors.ScriptedResponse{Status: http.StatusConflict})
			return harness.callTool("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
		}, func(result callResult) bool { return result.IsError && result.outcome() == nil }},
		{"rejected read", func() rpcResponse {
			return harness.callTool("example-notes_get_note", map[string]any{"noteId": "missing"}, "")
		}, func(result callResult) bool { return result.outcome() == outcomeRejected }},
		{"write without a key", func() rpcResponse {
			return harness.callTool("example-notes_post_note", map[string]any{"text": "a"}, "")
		}, func(result callResult) bool { return result.outcome() == outcomeRejected }},
		{"keyed write", func() rpcResponse {
			return harness.callTool("example-notes_post_note", map[string]any{"text": strings.Repeat("é", 900)}, "flow/turn/shape")
		}, func(result callResult) bool { return !result.IsError }},
		{"replayed write", func() rpcResponse {
			return harness.callTool("example-notes_post_note", map[string]any{"text": strings.Repeat("é", 900)}, "flow/turn/shape")
		}, func(result callResult) bool { return result.Meta[metaKeyReplayed] == true }},
		{"truncated read", func() rpcResponse {
			return harness.callTool("example-notes_get_note", map[string]any{"noteId": "n-1"}, "")
		}, func(result callResult) bool {
			return result.Meta[metaKeyTruncatedBytes] != nil && result.StructuredContent == nil
		}},
		{"unknown outcome", func() rpcResponse {
			harness.provider.Script("postNote", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
			return harness.callTool("example-notes_post_note", map[string]any{"text": "b"}, "flow/turn/shape-unknown")
		}, func(result callResult) bool { return result.outcome() == outcomeUnknown }},
		{"replay under a vanished name", func() rpcResponse {
			harness.writeConnections("work", "personal")
			return harness.callTool("example-notes_post_note", map[string]any{"text": strings.Repeat("é", 900)}, "flow/turn/shape")
		}, func(result callResult) bool { return result.Meta[metaKeyReplayed] == true }},
	}
	for _, testCase := range cases {
		response := testCase.call()
		require.Nil(t, response.Error, testCase.name)
		requireCallToolResultShape(t, testCase.name, response.Result, true)
		var result callResult
		require.NoError(t, json.Unmarshal(response.Result, &result))
		require.True(t, testCase.check(result), "%s: the case did not produce the result kind it names", testCase.name)
	}
}

func TestToolsCallResultsOmitResultTypeUnderOlderProtocols(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	for _, protocolVersion := range []string{"2025-11-25", "2025-06-18"} {
		body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
			"name": "example-notes_get_note", "arguments": map[string]any{"noteId": "welcome"},
		}}
		response := harness.postRPC(body, map[string]string{"MCP-Protocol-Version": protocolVersion})
		require.Nil(t, response.Error, protocolVersion)
		requireCallToolResultShape(t, protocolVersion, response.Result, false)
	}
}

func TestListingsAreCachedOnlyPrivately(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	response := harness.postRPC(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{
		"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": testProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}},
	}}, nil)
	require.Nil(t, response.Error)
	var listed struct {
		CacheScope string `json:"cacheScope"`
		ResultType string `json:"resultType"`
	}
	require.NoError(t, json.Unmarshal(response.Result, &listed))
	require.Equal(t, "private", listed.CacheScope, "the tool list depends on the user's authorized connections")
	require.Equal(t, "complete", listed.ResultType)
}

func TestGuardAndUnknownToolRejectionsLogTheirReason(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	mismatch := callWithMeta(harness, "example-notes_get_note", map[string]any{"noteId": "welcome"}, map[string]any{metaKeyExpectedConnection: "personal"})
	require.NotNil(t, mismatch.Error)
	unknown := harness.callTool("example-notes_missing", map[string]any{}, "")
	require.NotNil(t, unknown.Error)
	var reasons []string
	for _, line := range strings.Split(strings.TrimSpace(harness.logs.String()), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil || entry["msg"] != "tool call" {
			continue
		}
		require.Equal(t, outcomeRejected, entry["outcome"], line)
		reasons = append(reasons, entry["reason"].(string))
	}
	require.Equal(t, []string{"expectedIdentityMismatch", "unknownTool"}, reasons)
}
