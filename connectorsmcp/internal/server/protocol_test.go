// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// TestGoSDKClientsOfBothProtocolEras call the server like Dex's McpToolSource
// does: MCP 2026-07-28 without a session, and the initialize handshake.
func TestGoSDKClientsOfBothProtocolEras(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	for _, protocolVersion := range []string{"", "2025-11-25", "2025-06-18"} {
		client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
		session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: harness.httpServer.URL + MCPPath}, &mcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
		require.NoError(t, err, protocolVersion)
		listed, err := session.ListTools(t.Context(), nil)
		require.NoError(t, err, protocolVersion)
		require.Len(t, listed.Tools, 3, protocolVersion)
		read, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "example-notes_get_note", Arguments: map[string]any{"noteId": "welcome"}})
		require.NoError(t, err, protocolVersion)
		require.False(t, read.IsError, protocolVersion)
		written, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name: "example-notes_save_record", Arguments: map[string]any{"name": "a", "value": "1"},
			Meta: mcp.Meta{metaKeyIdempotencyKey: "flow/turn/" + protocolVersion},
		})
		require.NoError(t, err, protocolVersion)
		require.False(t, written.IsError, protocolVersion)
		refused, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "example-notes_post_note", Arguments: map[string]any{"text": "x"}})
		require.NoError(t, err, protocolVersion)
		require.True(t, refused.IsError)
		require.Equal(t, outcomeRejected, refused.Meta[metaKeyOutcome])
		require.NoError(t, session.Close())
	}
}
