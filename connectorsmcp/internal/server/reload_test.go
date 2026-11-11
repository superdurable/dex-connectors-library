// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
)

func toolNamesOf(harness *testHarness) []string {
	var names []string
	for _, tool := range harness.listTools() {
		names = append(names, tool["name"].(string))
	}
	slices.Sort(names)
	return names
}

func TestAddingAConnectionRenamesToolsButKeyedRetriesStillReplay(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	first := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "hello"}, "flow/turn/rename")
	require.False(t, first.IsError)

	harness.writeConnections("work", "personal")
	require.Equal(t, []string{
		"example-notes_personal__get_note", "example-notes_personal__post_note", "example-notes_personal__save_record",
		"example-notes_work__get_note", "example-notes_work__post_note", "example-notes_work__save_record",
	}, toolNamesOf(harness))
	require.NotEmpty(t, harness.toolSetChanges, "a changed tool set is reported for server.json")

	replay := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "hello"}, "flow/turn/rename")
	require.Equal(t, true, replay.Meta[metaKeyReplayed], "the key is looked up before the vanished name")
	require.Equal(t, first.text(), replay.text())

	response := harness.callTool("example-notes_post_note", map[string]any{"text": "new"}, "flow/turn/new")
	require.NotNil(t, response.Error)
	require.Equal(t, int64(jsonrpc.CodeInvalidParams), response.Error.Code)
	require.Equal(t, 1, harness.provider.NoteRequests())
}

func TestARunningIdempotentWriteWhoseToolVanishedIsFinalizedWithoutSending(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond})
	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	arguments := map[string]any{"name": "a", "value": "1"}
	interrupted := harness.callTool("example-notes_save_record", arguments, "flow/turn/vanish")
	require.NotNil(t, interrupted.Error)

	// Two other connections qualify every name, so the original name vanishes.
	harness.writeConnections("personal", "other")
	result := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/vanish")
	require.Equal(t, "unknown", result.outcome())
	require.Contains(t, result.text(), "no longer offered")
	require.Equal(t, 1, harness.provider.RecordRequests())
}

func TestAnInFlightCallKeepsTheToolSetItStartedWith(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	harness.provider.Script("postNote", testconnectors.ScriptedResponse{IsHeld: true})
	answers := make(chan callResult, 1)
	go func() {
		answers <- harness.mustCallResult("example-notes_post_note", map[string]any{"text": "slow"}, "flow/turn/inflight")
	}()
	require.Equal(t, "postNote", <-harness.provider.HeldRequests())
	harness.writeConnections("personal")
	require.Equal(t, []string{"example-notes_get_note", "example-notes_post_note", "example-notes_save_record"}, toolNamesOf(harness))
	harness.provider.Release()
	result := <-answers
	require.False(t, result.IsError, "the call ran on the connection it started with")
	require.JSONEq(t, `{"noteId":"n-1"}`, result.text())
}

func TestSelectionNamingCountsConfiguredConnectionsAndWarnsAboutUnauthorizedOnes(t *testing.T) {
	selection, err := NewConnectionSelection([]SelectedConnection{
		{ConnectorID: testconnectors.NotesConnectorID, ConnectionName: "work", Tools: []string{"get_note", "postNote"}},
		{ConnectorID: testconnectors.NotesConnectorID, ConnectionName: "personal"},
		{ConnectorID: "slack", ConnectionName: "work"},
		{ConnectorID: testconnectors.NotesConnectorID, ConnectionName: "spare", Tools: []string{"delete_note"}},
	})
	require.NoError(t, err)
	harness := newTestHarness(t, harnessOptions{selection: selection})
	require.Equal(t, []string{"example-notes_work__get_note", "example-notes_work__post_note"}, toolNamesOf(harness),
		"three configured notes connections qualify names although only work is authorized")
	warnings := strings.Join(harness.server.CurrentToolSet(t.Context()).Warnings(), "\n")
	require.Contains(t, warnings, "connection example-notes/personal is skipped: it is not authorized")
	require.Contains(t, warnings, "connection slack/work is skipped: connector slack is not included")
	require.Contains(t, warnings, "connection example-notes/spare is skipped: it is not authorized")
}

func TestSelectionToolListNamingAnUnexposedOperationWarns(t *testing.T) {
	selection, err := NewConnectionSelection([]SelectedConnection{
		{ConnectorID: testconnectors.NotesConnectorID, ConnectionName: "work", Tools: []string{"delete_note", "save_record"}},
	})
	require.NoError(t, err)
	harness := newTestHarness(t, harnessOptions{selection: selection})
	require.Equal(t, []string{"example-notes_save_record"}, toolNamesOf(harness))
	require.Contains(t, strings.Join(harness.server.CurrentToolSet(t.Context()).Warnings(), "\n"), "tool delete_note is skipped")
}

func TestCollidingToolNamesAreWithheld(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{connectionNames: []string{"a b", "a_b", "c"}})
	require.Equal(t, []string{"example-notes_c__get_note", "example-notes_c__post_note", "example-notes_c__save_record"}, toolNamesOf(harness))
	require.Contains(t, strings.Join(harness.server.CurrentToolSet(t.Context()).Warnings(), "\n"), "2 tools would be named example-notes_a_b__get_note; none of them is offered")
}

func TestRewritingTheSameConnectionsDoesNotChangeTheToolSet(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	before := harness.server.CurrentToolSet(t.Context())
	harness.writeConnections("work")
	later := time.Now().Add(time.Minute)
	require.NoError(t, os.Chtimes(harness.connectionsFile, later, later))
	after := harness.server.CurrentToolSet(t.Context())
	require.NotSame(t, before, after, "a rewritten file is reloaded")
	require.Equal(t, before.Digest(), after.Digest())
	require.Empty(t, harness.toolSetChanges, "an equal tool set is not reported, so a credential write-back never loops")
}

func TestAMissingConnectionsFileServesNoToolsUntilItAppears(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	require.NoError(t, os.Remove(harness.connectionsFile))
	require.Empty(t, harness.listTools())
	require.Contains(t, strings.Join(harness.server.CurrentToolSet(t.Context()).Warnings(), "\n"), "does not exist yet")
	harness.writeConnections("work")
	require.Len(t, harness.listTools(), 3)
}
