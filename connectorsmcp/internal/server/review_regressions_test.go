// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/callstate"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const differentCallText = "the idempotency key was used for a different call; nothing was sent"

func callWithMeta(harness *testHarness, name string, arguments any, extraMeta map[string]any) rpcResponse {
	harness.t.Helper()
	body := toolCallBody(name, arguments, "")
	meta := body["params"].(map[string]any)["_meta"].(map[string]any)
	for key, value := range extraMeta {
		meta[key] = value
	}
	return harness.postRPC(body, nil)
}

func requireRejectedError(t *testing.T, response rpcResponse, messagePart string) {
	t.Helper()
	require.NotNil(t, response.Error)
	require.Equal(t, int64(jsonrpc.CodeInvalidParams), response.Error.Code)
	require.JSONEq(t, `{"_meta":{"io.superdurable.dex/outcome":"rejected"}}`, string(response.Error.Data))
	require.Contains(t, response.Error.Message, messagePart)
}

func TestKeyReuseOnAnotherToolWithEqualArgumentsIsRejectedAndLeavesState(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{isChatLinked: true, callTimeBudget: 10500 * time.Millisecond})
	posted := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "x"}, "flow/turn/shared-key")
	require.False(t, posted.IsError)
	reused := harness.mustCallResult("example-chat_send_message", map[string]any{"text": "x"}, "flow/turn/shared-key")
	require.Equal(t, outcomeRejected, reused.outcome())
	require.Equal(t, differentCallText, reused.text())
	require.Zero(t, harness.chatSends.Count(), "the other tool never ran")
	replay := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "x"}, "flow/turn/shared-key")
	require.Equal(t, true, replay.Meta[metaKeyReplayed], "the record is unchanged")

	// A running idempotent write is never executed by another tool's call.
	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	arguments := map[string]any{"name": "a", "value": "1"}
	require.NotNil(t, harness.callTool("example-notes_save_record", arguments, "flow/turn/running-key").Error)
	recordPath := recordFilePath(harness.stateDirectory, "flow/turn/running-key")
	before, err := os.ReadFile(recordPath)
	require.NoError(t, err)
	runningReuse := harness.mustCallResult("example-chat_set_topic", arguments, "flow/turn/running-key")
	after, err := os.ReadFile(recordPath)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "the refused call left the record's bytes untouched")
	require.Equal(t, outcomeRejected, runningReuse.outcome())
	require.Zero(t, harness.chatSends.Count())
	require.Equal(t, 1, harness.provider.RecordRequests(), "the running write did not run for the other tool")
	resumed := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/running-key")
	require.False(t, resumed.IsError, "the running record still continues for its own call")
	require.Equal(t, 1, harness.provider.MaximumAppliedRecordWrites())
}

func TestKeyReuseOnAnotherConnectionWithEqualArgumentsIsRejected(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{connectionNames: []string{"work", "personal"}, callTimeBudget: 10500 * time.Millisecond})
	posted := harness.mustCallResult("example-notes_work__post_note", map[string]any{"text": "x"}, "flow/turn/connection-key")
	require.False(t, posted.IsError)
	reused := harness.mustCallResult("example-notes_personal__post_note", map[string]any{"text": "x"}, "flow/turn/connection-key")
	require.Equal(t, differentCallText, reused.text())
	require.Equal(t, 1, harness.provider.NoteRequests())

	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	arguments := map[string]any{"name": "a", "value": "1"}
	require.NotNil(t, harness.callTool("example-notes_work__save_record", arguments, "flow/turn/connection-running").Error)
	runningReuse := harness.mustCallResult("example-notes_personal__save_record", arguments, "flow/turn/connection-running")
	require.Equal(t, differentCallText, runningReuse.text())
	require.Equal(t, 1, harness.provider.RecordRequests(), "the running write did not run on the other connection")
}

func TestKeyReuseFromAnotherProjectWithEqualArgumentsIsRejected(t *testing.T) {
	first := newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond})
	posted := first.mustCallResult("example-notes_post_note", map[string]any{"text": "x"}, "flow/turn/project-key")
	require.False(t, posted.IsError)
	first.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	arguments := map[string]any{"name": "a", "value": "1"}
	require.NotNil(t, first.callTool("example-notes_save_record", arguments, "flow/turn/project-running").Error)

	second := newTestHarness(t, harnessOptions{stateDirectory: first.stateDirectory})
	require.NotEqual(t, first.server.CurrentToolSet(t.Context()).ProjectIdentity(), second.server.CurrentToolSet(t.Context()).ProjectIdentity())
	reused := second.mustCallResult("example-notes_post_note", map[string]any{"text": "x"}, "flow/turn/project-key")
	require.Equal(t, differentCallText, reused.text())
	runningReuse := second.mustCallResult("example-notes_save_record", arguments, "flow/turn/project-running")
	require.Equal(t, differentCallText, runningReuse.text())
	require.Zero(t, second.provider.NoteRequests())
	require.Zero(t, second.provider.RecordRequests())
	replay := first.mustCallResult("example-notes_post_note", map[string]any{"text": "x"}, "flow/turn/project-key")
	require.Equal(t, true, replay.Meta[metaKeyReplayed])
}

func TestARemappedToolNameIsADifferentCallUnlessTheExpectedIdentityNamesTheRecord(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	arguments := map[string]any{"text": "x"}
	posted := harness.mustCallResult("example-notes_post_note", arguments, "flow/turn/remap")
	require.False(t, posted.IsError)
	// The base name now means the personal connection.
	harness.writeConnections("personal")
	remapped := harness.mustCallResult("example-notes_post_note", arguments, "flow/turn/remap")
	require.Equal(t, differentCallText, remapped.text())

	expected := map[string]any{metaKeyIdempotencyKey: "flow/turn/remap", metaKeyExpectedConnector: testconnectors.NotesConnectorID, metaKeyExpectedConnection: "work", metaKeyExpectedOperation: "postNote"}
	var replay callResult
	response := callWithMeta(harness, "example-notes_post_note", arguments, expected)
	require.Nil(t, response.Error)
	require.NoError(t, json.Unmarshal(response.Result, &replay))
	require.Equal(t, true, replay.Meta[metaKeyReplayed], "the expected identity names the record's call")
	require.Equal(t, 1, harness.provider.NoteRequests())
}

func TestAListedRejectedBranchAfterAnEarlierSendStaysRejected(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	harness.provider.Script("saveRecord",
		testconnectors.ScriptedResponse{IsEffectAppliedFirst: true, Status: http.StatusServiceUnavailable},
		testconnectors.ScriptedResponse{Status: http.StatusBadRequest},
	)
	harness.provider.Script("readRecord", testconnectors.ScriptedResponse{Status: http.StatusNotFound})
	result := harness.mustCallResult("example-notes_save_record", map[string]any{"name": "a", "value": "1"}, "flow/turn/applied-then-rejected")
	// Dex Agents classify by the selected branch only; the connector owns repeat safety.
	require.Equal(t, outcomeRejected, result.outcome(), result.text())
	require.NotContains(t, result.text(), "nothing was sent")
	require.Equal(t, 1, harness.provider.AppliedRecordWrites(saveRecordIdempotencyKey("flow/turn/applied-then-rejected")))
}

func TestAnUnsettledCallIsRejectedOnlyWhenNoAttemptInvokedTheOperation(t *testing.T) {
	store, err := callstate.Open(filepath.Join(t.TempDir(), "state"), time.Now)
	require.NoError(t, err)
	executor := newToolCallExecutor(store, newFakeClock(), executorConfig{serverVersion: "test", callTimeBudget: time.Minute, callStateRetention: 720 * time.Hour, maxResultBytes: 65536, maximumInlineAttempts: 3}, slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context) *ToolSet { return &ToolSet{} }, nil)
	for _, testCase := range []struct {
		name              string
		isPossiblyApplied bool
		expectedOutcome   string
	}{
		{"never invoked", false, outcomeRejected},
		{"an earlier attempt invoked the operation", true, outcomeUnknown},
	} {
		key := "flow/turn/" + testCase.name
		call := &toolCall{executor: executor, stateKey: callstate.StateKey(key)}
		lock, err := store.Lock(t.Context(), call.stateKey)
		require.NoError(t, err)
		record := runningRecord(call.stateKey, testCase.isPossiblyApplied)
		require.NoError(t, lock.Write(&record))
		answer := call.finalizeWithoutOutcome(lock, &record, "it reached 3 attempts")
		require.NoError(t, lock.Release())
		require.Equal(t, testCase.expectedOutcome, answer.result.Meta[metaKeyOutcome], testCase.name)
	}

	for _, testCase := range []struct {
		isPossiblyApplied bool
		expectedOutcome   any
	}{
		{false, outcomeRejected},
		{true, nil},
	} {
		call := &toolCall{executor: executor, name: "example-notes_save_record", stateKey: callstate.StateKey(fmt.Sprintf("flow/turn/invalid/%t", testCase.isPossiblyApplied))}
		lock, err := store.Lock(t.Context(), call.stateKey)
		require.NoError(t, err)
		record := runningRecord(call.stateKey, testCase.isPossiblyApplied)
		require.NoError(t, lock.Write(&record))
		answer := call.finishWriteAttemptWithoutBranch(lock, &record, nil, sdkgo.AgentToolAttempt{}, sdkgo.ErrAgentToolArgumentsInvalid)
		require.NoError(t, lock.Release())
		require.True(t, answer.result.IsError)
		require.Equal(t, testCase.expectedOutcome, answer.result.Meta[metaKeyOutcome], "invalid arguments after an earlier send fail without claiming nothing took effect")
		require.Contains(t, answer.result.Content[0].Text, "invalid arguments")
	}

	retry := sdkgo.AgentToolAttempt{Outcome: sdkgo.AgentToolAttemptRetry, HasInvokedOperation: true}
	require.True(t, attemptMayHaveApplied(retry, nil), "a Retry after Invoke may have sent")
	require.False(t, attemptMayHaveApplied(sdkgo.AgentToolAttempt{Outcome: sdkgo.AgentToolAttemptRetry}, nil), "a Retry before Invoke sent nothing")
	require.False(t, attemptMayHaveApplied(sdkgo.AgentToolAttempt{Outcome: sdkgo.AgentToolAttemptRejected, HasInvokedOperation: true}, nil))
	require.True(t, attemptMayHaveApplied(sdkgo.AgentToolAttempt{Outcome: sdkgo.AgentToolAttemptUnknown}, nil))
	require.True(t, attemptMayHaveApplied(sdkgo.AgentToolAttempt{HasInvokedOperation: true}, sdkgo.ErrAgentToolPanicked))
	require.False(t, attemptMayHaveApplied(sdkgo.AgentToolAttempt{}, sdkgo.ErrAgentToolPanicked))
}

func TestARetryWindowThatPassesDuringTheDelayStopsAnotherSend(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	harness.clock.setOversleep(time.Hour)
	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	result := harness.mustCallResult("example-notes_save_record", map[string]any{"name": "a", "value": "1"}, "flow/turn/window-after-sleep")
	require.Equal(t, outcomeUnknown, result.outcome(), "attempt 1 reached the provider")
	require.Contains(t, result.text(), "retry window passed while it waited to retry")
	require.Equal(t, 1, harness.provider.RecordRequests(), "no send after the window")
}

func TestASlowAttemptIsCutAtTheRetryWindow(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	// After the 1s delay, 300ms of the one-hour window remain.
	harness.clock.setOversleep(time.Hour - time.Second - 300*time.Millisecond)
	harness.provider.Script("saveRecord",
		testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable},
		testconnectors.ScriptedResponse{IsHeld: true},
	)
	startedAt := time.Now()
	result := harness.mustCallResult("example-notes_save_record", map[string]any{"name": "a", "value": "1"}, "flow/turn/window-slow")
	require.Less(t, time.Since(startedAt), 5*time.Second, "the attempt deadline is the window end, not the 10s attempt timeout")
	require.Equal(t, outcomeUnknown, result.outcome())
	require.Equal(t, 2, harness.provider.RecordRequests())
	require.Zero(t, harness.provider.MaximumAppliedRecordWrites(), "the held send was cancelled at the window end")
}

func TestExpectedIdentityGuardWithoutState(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	matching := callWithMeta(harness, "example-notes_get_note", map[string]any{"noteId": "welcome"}, map[string]any{
		metaKeyExpectedConnector: testconnectors.NotesConnectorID, metaKeyExpectedConnection: "work", metaKeyExpectedOperation: "getNote",
	})
	require.Nil(t, matching.Error)
	require.Equal(t, 1, harness.provider.ReadRequests())

	mismatch := callWithMeta(harness, "example-notes_get_note", map[string]any{"noteId": "welcome"}, map[string]any{metaKeyExpectedConnection: "personal"})
	requireRejectedError(t, mismatch, metaKeyExpectedConnection)
	require.Equal(t, 1, harness.provider.ReadRequests(), "a mismatch never runs the tool")

	keyedMismatch := callWithMeta(harness, "example-notes_post_note", map[string]any{"text": "x"}, map[string]any{metaKeyIdempotencyKey: "flow/turn/guard", metaKeyExpectedOperation: "deleteNote"})
	requireRejectedError(t, keyedMismatch, metaKeyExpectedOperation)
	notString := callWithMeta(harness, "example-notes_post_note", map[string]any{"text": "x"}, map[string]any{metaKeyIdempotencyKey: "flow/turn/guard", metaKeyExpectedConnector: 7})
	requireRejectedError(t, notString, metaKeyExpectedConnector)
	require.Zero(t, harness.provider.NoteRequests())
	first := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "x"}, "flow/turn/guard")
	require.False(t, first.IsError)
	require.Nil(t, first.Meta[metaKeyReplayed], "the refused calls wrote no state for the key")
}

func TestExpectedIdentityGuardWithState(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	arguments := map[string]any{"text": "x"}
	posted := harness.mustCallResult("example-notes_post_note", arguments, "flow/turn/guarded")
	require.False(t, posted.IsError)

	mismatch := callWithMeta(harness, "example-notes_post_note", arguments, map[string]any{metaKeyIdempotencyKey: "flow/turn/guarded", metaKeyExpectedConnector: "example-chat"})
	requireRejectedError(t, mismatch, metaKeyExpectedConnector)
	matching := callWithMeta(harness, "example-notes_post_note", arguments, map[string]any{metaKeyIdempotencyKey: "flow/turn/guarded", metaKeyExpectedConnection: "work", metaKeyExpectedOperation: "postNote"})
	require.Nil(t, matching.Error)
	var replay callResult
	require.NoError(t, json.Unmarshal(matching.Result, &replay))
	require.Equal(t, true, replay.Meta[metaKeyReplayed], "the refused call left the record unchanged")
	require.Equal(t, 1, harness.provider.NoteRequests())
}

func TestEmptyToolListsFailClosed(t *testing.T) {
	_, err := ParseConnectionFlag("example-notes/work=")
	require.ErrorContains(t, err, "example-notes/work=")
	_, err = ParseConnectionsConfig([]byte(`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[{"connection":"example-notes/work","tools":[]}]}`))
	require.ErrorContains(t, err, "connection example-notes/work lists no tools")
	selection, err := ParseConnectionsConfig([]byte(`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[{"connection":"example-notes/work"}]}`))
	require.NoError(t, err)
	require.Nil(t, selection.Connections()[0].Tools, "only an absent list means every exposed tool")
	_, err = ParseConnectionsConfig([]byte(`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[]}{}`))
	require.ErrorContains(t, err, "data after its JSON document")
}

// saveRecordIdempotencyKey is the provider idempotency key saveRecord sends for an MCP key.
func saveRecordIdempotencyKey(key string) string {
	return string(deriveCallID(localFlowID, key, sdkgo.OperationRef{ConnectorID: testconnectors.NotesConnectorID, OperationID: "saveRecord"}))
}

func recordFilePath(stateDirectory string, key string) string {
	stateKey := callstate.StateKey(key)
	return filepath.Join(stateDirectory, "calls", "v1", stateKey[:2], stateKey+".json")
}

// runningRecord's checkpoint is opaque connector state; the server never reads it.
func runningRecord(stateKey string, isPossiblyApplied bool) callstate.Record {
	checkpoint := json.RawMessage(`{"connectorState":"notSent"}`)
	request := callstate.Request{ConnectorID: "example-notes", OperationID: "saveRecord", ConnectionName: "work", ProjectIdentity: "file:/p", ArgumentsDigest: strings.Repeat("a", 64)}
	first := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	return callstate.Record{
		SchemaVersion: callstate.FormatVersion, StateKey: stateKey, Request: request, RequestDigest: request.Digest(),
		Effect: sdkgo.AgentToolEffectIdempotentWrite, CallID: "call-1", ConnectionTarget: "sha256:" + strings.Repeat("c", 64), FirstAttemptAt: first, RetryWindowEndsAt: first.Add(time.Hour),
		Attempt: 3, Checkpoint: checkpoint, PossiblyApplied: isPossiblyApplied, Status: callstate.StatusRunning, ServerVersion: "test",
	}
}
