// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/callstate"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestConnectionTargetDigestUsesOnlyNonSecretTargetFields(t *testing.T) {
	slack := projectconfig.ConnectionConfiguration{
		ConnectorID: "slack", ConnectionName: "work", ModulePath: "example.com/slack/go", Provider: "slack", AuthMethodID: "oauth",
		Configuration: json.RawMessage(`{ "workspace": "T123", "baseUrl": "https://slack.com/api" }`),
	}
	// Computed independently with Python json.dumps(sort_keys=True, separators=(",", ":")).
	require.Equal(t, "sha256:a9082ed1e9c48cb3e4e02d7311e439922b7587461cc21938b2a8a4992f65658e", connectionTargetDigest(slack))
	hackerNews := projectconfig.ConnectionConfiguration{ConnectorID: "hacker-news-daily", ConnectionName: "default", Provider: "hacker-news"}
	require.Equal(t, "sha256:f60918fde0944e9683a9ff3cf8b3fa7f434e93e5138edd2c2b6f907d2c9561e5", connectionTargetDigest(hackerNews))

	sameTarget := slack
	sameTarget.ModulePath = "example.com/slack/go/v2"
	sameTarget.Configuration = json.RawMessage(`{"baseUrl":"https://slack.com/api","workspace":"T123"}`)
	require.Equal(t, connectionTargetDigest(slack), connectionTargetDigest(sameTarget), "key order, whitespace, and a module move keep the target")
	for name, change := range map[string]func(*projectconfig.ConnectionConfiguration){
		"another endpoint": func(c *projectconfig.ConnectionConfiguration) {
			c.Configuration = json.RawMessage(`{"baseUrl":"https://other","workspace":"T123"}`)
		},
		"another workspace": func(c *projectconfig.ConnectionConfiguration) {
			c.Configuration = json.RawMessage(`{"baseUrl":"https://slack.com/api","workspace":"T999"}`)
		},
		"another provider":          func(c *projectconfig.ConnectionConfiguration) { c.Provider = "slack-enterprise" },
		"another auth method":       func(c *projectconfig.ConnectionConfiguration) { c.AuthMethodID = "botToken" },
		"another shared connection": func(c *projectconfig.ConnectionConfiguration) { c.SharedConnectionID = "shared-1" },
	} {
		changed := slack
		change(&changed)
		require.NotEqual(t, connectionTargetDigest(slack), connectionTargetDigest(changed), name)
	}
}

func TestARebindToAnotherEndpointSettlesARunningKeyWithoutSending(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond})
	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	arguments := map[string]any{"name": "a", "value": "1"}
	require.NotNil(t, harness.callTool("example-notes_save_record", arguments, "flow/turn/rebind").Error, "the key stays running")

	otherAccount := testconnectors.NewFakeProvider()
	t.Cleanup(otherAccount.Close)
	harness.writeConnectionRecords(testconnectors.NotesConnection("work", otherAccount))
	settled := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/rebind")
	require.Equal(t, outcomeUnknown, settled.outcome(), "attempt 1 reached the first target")
	require.Contains(t, settled.text(), "the connection now points to a different target")
	require.Zero(t, otherAccount.TotalRequests(), "nothing ran against the new target")
	require.Equal(t, 1, harness.provider.RecordRequests())
	replay := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/rebind")
	require.Equal(t, true, replay.Meta[metaKeyReplayed])
	require.Zero(t, otherAccount.TotalRequests())

	fresh := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/after-rebind")
	require.False(t, fresh.IsError, "a new key runs against the new target")
	require.Equal(t, 1, otherAccount.AppliedRecordWrites(saveRecordIdempotencyKey("flow/turn/after-rebind")))
}

func TestARebindDuringTheRetryDelayStopsTheNextAttempt(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	otherAccount := testconnectors.NewFakeProvider()
	t.Cleanup(otherAccount.Close)
	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	harness.clock.setOnSleep(func() { harness.writeConnectionRecords(testconnectors.NotesConnection("work", otherAccount)) })
	result := harness.mustCallResult("example-notes_save_record", map[string]any{"name": "a", "value": "1"}, "flow/turn/rebind-in-delay")
	require.Equal(t, outcomeUnknown, result.outcome())
	require.Contains(t, result.text(), "the connection now points to a different target")
	require.Zero(t, otherAccount.TotalRequests())
	require.Equal(t, 1, harness.provider.RecordRequests())
}

func TestRefreshingTheSameTargetKeepsARunningKey(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond})
	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	arguments := map[string]any{"name": "a", "value": "1"}
	require.NotNil(t, harness.callTool("example-notes_save_record", arguments, "flow/turn/refresh").Error)
	before := harness.server.CurrentToolSet(t.Context()).Digest()

	refreshed := testconnectors.NotesConnection("work", harness.provider)
	expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	refreshed.CredentialExpiresAt = &expiresAt
	harness.writeConnectionRecords(refreshed)
	require.Equal(t, before, harness.server.CurrentToolSet(t.Context()).Digest(), "a refreshed credential keeps the tool set")
	resumed := harness.mustCallResult("example-notes_save_record", arguments, "flow/turn/refresh")
	require.False(t, resumed.IsError, "the same target keeps running")
	require.Equal(t, 2, harness.provider.RecordRequests())
	require.Equal(t, 1, harness.provider.AppliedRecordWrites(saveRecordIdempotencyKey("flow/turn/refresh")))
}

func TestARetargetedKeyWithNotSentProofIsRejected(t *testing.T) {
	store, err := callstate.Open(filepath.Join(t.TempDir(), "state"), time.Now)
	require.NoError(t, err)
	executor := newToolCallExecutor(store, newFakeClock(), executorConfig{serverVersion: "test", callTimeBudget: time.Minute, callStateRetention: 720 * time.Hour, maxResultBytes: 65536, maximumInlineAttempts: 3},
		slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context) *ToolSet { return &ToolSet{} }, nil)
	for _, testCase := range []struct {
		isPossiblyApplied bool
		expectedOutcome   string
		expectedText      string
	}{
		{false, outcomeRejected, "example-notes saveRecord did not take effect: the connection now points to a different target; nothing was sent"},
		{true, outcomeUnknown, "example-notes saveRecord may have taken effect: the connection now points to a different target; do not repeat it"},
	} {
		call := &toolCall{executor: executor, stateKey: callstate.StateKey("flow/turn/retarget/" + testCase.expectedOutcome)}
		lock, err := store.Lock(t.Context(), call.stateKey)
		require.NoError(t, err)
		record := notSentRecord(call.stateKey, testCase.isPossiblyApplied)
		require.NoError(t, lock.Write(&record))
		answer := call.finalizeFromCheckpoint(lock, &record, connectionRetargetedReason)
		require.NoError(t, lock.Release())
		require.Equal(t, testCase.expectedOutcome, answer.result.Meta[metaKeyOutcome])
		require.Equal(t, testCase.expectedText, answer.result.Content[0].Text)
	}
}

func TestARebindDuringAReadRetryDelayStopsTheRead(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	otherAccount := testconnectors.NewFakeProvider()
	t.Cleanup(otherAccount.Close)
	harness.provider.Script("getNote", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	harness.clock.setOnSleep(func() { harness.writeConnectionRecords(testconnectors.NotesConnection("work", otherAccount)) })
	result := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "welcome"}, "")
	require.Equal(t, outcomeRejected, result.outcome())
	require.Contains(t, result.text(), "now points to a different target")
	require.Equal(t, 1, harness.provider.ReadRequests(), "no attempt reused the old call's settings")
	require.Zero(t, otherAccount.TotalRequests(), "no attempt ran against the new target")
}

func TestARefreshDuringAReadRetryDelayKeepsTheRead(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	harness.provider.Script("getNote", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	harness.clock.setOnSleep(func() {
		refreshed := testconnectors.NotesConnection("work", harness.provider)
		expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
		refreshed.CredentialExpiresAt = &expiresAt
		harness.writeConnectionRecords(refreshed)
	})
	result := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "welcome"}, "flow/turn/read-refresh")
	require.False(t, result.IsError, result.text())
	require.Equal(t, 2, harness.provider.ReadRequests())
}
