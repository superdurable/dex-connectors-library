// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
)

func TestToolsListCarriesDexConventionsIdentityAndAnnotations(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	tools := harness.listTools()
	byName := map[string]map[string]any{}
	for _, tool := range tools {
		byName[tool["name"].(string)] = tool
	}
	require.ElementsMatch(t, []string{"example-notes_get_note", "example-notes_post_note", "example-notes_save_record"}, keysOf(byName),
		"deleteNote is not exposed and must not be offered")

	expectations := map[string]struct {
		operation, operationName, effect string
		isApprovalRecommended            bool
		annotations                      map[string]any
	}{
		"example-notes_get_note":    {"getNote", "get_note", "read", false, map[string]any{"readOnlyHint": true, "idempotentHint": false, "openWorldHint": true}},
		"example-notes_post_note":   {"postNote", "post_note", "non_idempotent_write", true, map[string]any{"readOnlyHint": false, "idempotentHint": false, "destructiveHint": true, "openWorldHint": true}},
		"example-notes_save_record": {"saveRecord", "save_record", "idempotent_write", false, map[string]any{"readOnlyHint": false, "idempotentHint": true, "openWorldHint": true}},
	}
	for name, expected := range expectations {
		tool := byName[name]
		meta := tool["_meta"].(map[string]any)
		require.Equal(t, expected.effect, meta[metaKeyEffect], name)
		require.Equal(t, testconnectors.NotesConnectorID, meta[metaKeyConnector], name)
		require.Equal(t, "work", meta[metaKeyConnection], name)
		require.Equal(t, expected.operation, meta[metaKeyOperation], name)
		require.Equal(t, expected.operationName, meta[metaKeyOperationName], name)
		require.Equal(t, testconnectors.NotesModuleVersion, meta[metaKeyConnectorVersion], name)
		approval, hasApproval := meta[metaKeyApproval]
		if expected.isApprovalRecommended {
			require.Equal(t, "recommended", approval, name)
		} else {
			require.False(t, hasApproval, "%s: Dex asks for approval whenever the approval key is present, so it must be absent", name)
		}
		require.Equal(t, expected.annotations, tool["annotations"], name)
		require.NotEmpty(t, tool["description"], name)
		require.True(t, strings.HasPrefix(tool["title"].(string), testconnectors.NotesDisplayName+": "), name)
		require.Equal(t, "object", tool["inputSchema"].(map[string]any)["type"], name)
	}
}

func TestReadSucceedsWithCanonicalTextAndStructuredContent(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	result := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
	require.False(t, result.IsError)
	require.Nil(t, result.outcome())
	require.Equal(t, `{"id":"welcome","text":"Welcome <to> the notes & more"}`, result.text(), "sorted keys and no HTML escaping")
	require.JSONEq(t, result.text(), string(result.StructuredContent))
	require.Equal(t, "found", result.Meta[metaKeyBranch])
}

func TestReadBranchesMapToRejectedAndFailed(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	rejected := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "missing"}, "")
	require.True(t, rejected.IsError)
	require.Equal(t, "rejected", rejected.outcome())
	require.Equal(t, "example-notes getNote returned notFound: NOT_FOUND: no note has this ID", rejected.text())

	harness.provider.Script("getNote", testconnectors.ScriptedResponse{Status: http.StatusConflict})
	failed := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
	require.True(t, failed.IsError)
	require.Nil(t, failed.outcome(), "an unlisted Query branch is failed: isError without the Dex outcome key")
	require.Equal(t, "unavailable", failed.Meta[metaKeyBranch])
}

func TestReadRetriesInlineWithPolicyDelaysAndRetryAfter(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	harness.provider.Script("getNote",
		testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable},
		testconnectors.ScriptedResponse{Status: http.StatusTooManyRequests, RetryAfter: 7 * time.Second},
	)
	result := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
	require.False(t, result.IsError)
	require.Equal(t, []time.Duration{time.Second, 7 * time.Second}, harness.clock.recordedSleeps(), "1s initial delay, then RetryAfter when longer than 2s")
	require.Equal(t, 3, harness.provider.ReadRequests())
}

func TestReadAtTheAttemptCapIsRejected(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	for range 3 {
		harness.provider.Script("getNote", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	}
	result := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
	require.Equal(t, "rejected", result.outcome())
	require.Contains(t, result.text(), "after 3 attempts")
	require.Equal(t, 3, harness.provider.ReadRequests())
}

func TestReadWhoseBudgetIsSpentIsInterrupted(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond})
	harness.provider.Script("getNote", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	response := harness.callTool("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
	require.NotNil(t, response.Error)
	require.Equal(t, int64(jsonrpc.CodeInternalError), response.Error.Code, "a 1s delay plus a 10s attempt does not fit in 10.5s")
	require.Empty(t, response.Error.Data, "a bare internal error carries no outcome, so Dex re-issues the read")
}

func TestUnknownToolIsInvalidParamsWithRejectedOutcome(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	for _, key := range []string{"", "flow/turn/unknown"} {
		response := harness.callTool("example-notes_missing", map[string]any{}, key)
		require.NotNil(t, response.Error)
		require.Equal(t, int64(jsonrpc.CodeInvalidParams), response.Error.Code)
		require.JSONEq(t, `{"_meta":{"io.superdurable.dex/outcome":"rejected"}}`, string(response.Error.Data))
		// MCP 2026-07-28 (SEP-2575) sends -32602 with HTTP 400; the MCP Python SDK reads the JSON-RPC body.
		require.Equal(t, http.StatusBadRequest, response.HTTPStatus)
	}
}

func TestInvalidArgumentsAreRejectedWithoutState(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	result := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": ""}, "")
	require.Equal(t, "rejected", result.outcome())
	require.True(t, strings.HasPrefix(result.text(), "invalid arguments for example-notes_get_note: "))

	write := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "hi", "extra": true}, "flow/turn/invalid")
	require.Equal(t, "rejected", write.outcome())
	require.Equal(t, 0, harness.provider.NoteRequests())
	retry := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "hi"}, "flow/turn/invalid")
	require.False(t, retry.IsError, "invalid arguments wrote no state, so the key is still free")
}

func TestWritesWithoutAnIdempotencyKeyAreRefused(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	for _, name := range []string{"example-notes_post_note", "example-notes_save_record"} {
		result := harness.mustCallResult(name, map[string]any{"text": "hi", "name": "a", "value": "b"}, "")
		require.Equal(t, "rejected", result.outcome(), name)
		require.Contains(t, result.text(), "needs _meta io.superdurable.dex/idempotencyKey; nothing was sent")
	}
	require.Equal(t, 0, harness.provider.NoteRequests())
	require.Equal(t, 0, harness.provider.RecordRequests())
}

func TestMalformedIdempotencyKeyIsRejected(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	response := harness.postRPC(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
		"name": "example-notes_post_note", "arguments": map[string]any{"text": "hi"},
		"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": testProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}, metaKeyIdempotencyKey: 42},
	}}, nil)
	var result callResult
	require.NoError(t, json.Unmarshal(response.Result, &result))
	require.Equal(t, "rejected", result.outcome())
	tooLong := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "hi"}, strings.Repeat("k", 1025))
	require.Equal(t, "rejected", tooLong.outcome())
	require.Equal(t, 0, harness.provider.NoteRequests())
}

func TestNonIdempotentWriteSendsOnceAndReplays(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	first := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "hello"}, "flow/turn/post-1")
	require.False(t, first.IsError)
	require.JSONEq(t, `{"noteId":"n-1"}`, first.text())
	replay := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "hello"}, "flow/turn/post-1")
	require.Equal(t, first.text(), replay.text())
	require.Equal(t, true, replay.Meta[metaKeyReplayed])
	require.Equal(t, 1, harness.provider.NoteRequests())

	reused := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "other"}, "flow/turn/post-1")
	require.Equal(t, "rejected", reused.outcome())
	require.Contains(t, reused.text(), "was used for a different call; nothing was sent")
	require.Equal(t, 1, harness.provider.NoteRequests())
}

func TestNonIdempotentWriteRejectedBeforeSendingIsStoredRejected(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	result := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "  "}, "flow/turn/blank")
	require.Equal(t, "rejected", result.outcome(), "the connector refused before the Fence claimed, so the listing applies")
	require.Equal(t, "invalidText", result.Meta[metaKeyBranch])
	replay := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "  "}, "flow/turn/blank")
	require.Equal(t, true, replay.Meta[metaKeyReplayed])
	require.Equal(t, "rejected", replay.outcome())
	require.Equal(t, 0, harness.provider.NoteRequests())
}

func TestNonIdempotentWriteRetryAfterSendIsUnknown(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	harness.provider.Script("postNote", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	result := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "hello"}, "flow/turn/post-503")
	require.Equal(t, "unknown", result.outcome(), "canResend follows a claim and does not prove the note was not created")
	require.Empty(t, harness.clock.recordedSleeps(), "a non-idempotent write is never retried inline")
	again := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "hello"}, "flow/turn/post-503")
	require.Equal(t, true, again.Meta[metaKeyReplayed])
	require.Equal(t, 1, harness.provider.NoteRequests())
}

func TestIdempotentWriteRetriesInlineUnderOneKey(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	result := harness.mustCallResult("example-notes_save_record", map[string]any{"name": "a", "value": "1"}, "flow/turn/save-1")
	require.False(t, result.IsError)
	require.JSONEq(t, `{"value":"1"}`, result.text())
	require.Equal(t, []time.Duration{time.Second}, harness.clock.recordedSleeps())
	require.Equal(t, 1, harness.provider.MaximumAppliedRecordWrites())
}

func TestIdempotentWriteListedRejectionIsRejected(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	result := harness.mustCallResult("example-notes_save_record", map[string]any{"name": "forbidden", "value": "1"}, "flow/turn/save-forbidden")
	require.Equal(t, "rejected", result.outcome())
	require.Equal(t, "example-notes saveRecord returned providerRejected: PROVIDER_REJECTION: the record store refused the record", result.text())
}

func TestIdempotentWriteAttemptCapHoldsAcrossReissues(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond})
	for range 5 {
		harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	}
	arguments := map[string]any{"name": "a", "value": "1"}
	for attempt := 1; attempt <= 2; attempt++ {
		response := harness.callTool("example-notes_save_record", arguments, "flow/turn/save-cap")
		require.NotNil(t, response.Error, "attempt %d: the budget allows no inline retry", attempt)
		require.Equal(t, int64(jsonrpc.CodeInternalError), response.Error.Code)
	}
	final := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/save-cap")
	require.Equal(t, "unknown", final.outcome(), "the third persisted attempt is the cap; no notSent proof, so unknown")
	replay := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/save-cap")
	require.Equal(t, true, replay.Meta[metaKeyReplayed])
	require.Equal(t, 3, harness.provider.RecordRequests(), "exactly three attempts reached the provider")
}

func TestIdempotentWriteRetryWindowIsFrozenAtTheFirstAttempt(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond})
	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	arguments := map[string]any{"name": "a", "value": "1"}
	response := harness.callTool("example-notes_save_record", arguments, "flow/turn/save-window")
	require.NotNil(t, response.Error)
	harness.clock.advance(time.Hour)
	final := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/save-window")
	require.Equal(t, "unknown", final.outcome())
	require.Contains(t, final.text(), "retry window has passed")
	require.Equal(t, 1, harness.provider.RecordRequests(), "no attempt starts after the window")
}

func TestKeyedReadKeepsNoState(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	for range 2 {
		result := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "welcome"}, "flow/turn/read-1")
		require.False(t, result.IsError)
		require.Nil(t, result.Meta[metaKeyReplayed])
	}
	require.Equal(t, 2, harness.provider.ReadRequests())
}

func TestLargeResultsAreTruncatedWithoutStructuredContent(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{maxResultBytes: 1024})
	longText := strings.Repeat("é", 900)
	posted := harness.mustCallResult("example-notes_post_note", map[string]any{"text": longText}, "flow/turn/long")
	require.False(t, posted.IsError)
	result := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "n-1"}, "")
	require.False(t, result.IsError)
	require.Nil(t, result.StructuredContent)
	fullSize := len(`{"id":"n-1","text":"` + longText + `"}`)
	require.Equal(t, float64(fullSize), result.Meta[metaKeyTruncatedBytes])
	require.True(t, strings.HasSuffix(result.text(), "\n[Dex] truncated, "+itoa(fullSize)+" bytes"))
	kept := strings.Split(result.text(), "\n")[0]
	require.LessOrEqual(t, len(kept), 1024)
	require.True(t, strings.ToValidUTF8(kept, "?") == kept, "the cut keeps whole characters")
}

func TestDrainingRejectsNewNonIdempotentWritesAndInterruptsOthers(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	completed := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "before"}, "flow/turn/before-drain")
	require.False(t, completed.IsError)
	harness.server.BeginDrain()
	rejected := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "x"}, "flow/turn/drain")
	require.Equal(t, "rejected", rejected.outcome())
	require.Contains(t, rejected.text(), "server is stopping; nothing was sent")
	replayed := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "before"}, "flow/turn/before-drain")
	require.Equal(t, true, replayed.Meta[metaKeyReplayed], "a completed key replays while draining")
	for _, name := range []string{"example-notes_save_record", "example-notes_get_note"} {
		response := harness.callTool(name, map[string]any{"name": "a", "value": "b", "noteId": "welcome"}, "flow/turn/drain-"+name)
		if name == "example-notes_get_note" {
			response = harness.callTool(name, map[string]any{"noteId": "welcome"}, "")
		}
		require.NotNil(t, response.Error, name)
		require.Equal(t, int64(jsonrpc.CodeInternalError), response.Error.Code, name)
	}
	require.Equal(t, 1, harness.provider.NoteRequests())
}

func keysOf(values map[string]map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func itoa(value int) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
