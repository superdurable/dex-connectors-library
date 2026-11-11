// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"context"
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

func TestTheConnectionTargetIsTheProjectConfigFingerprint(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	base := testconnectors.NotesConnection("work", harness.provider)
	base.AuthorizationID = "8f14e45f-ceea-4e7a-9b1d-3c59e6a1b2c4"
	harness.writeConnectionRecords(base)
	targetOf := func() string {
		binding := harness.server.CurrentToolSet(t.Context()).toolByName("example-notes_save_record")
		require.NotNil(t, binding)
		return binding.connectionTarget
	}
	project, err := projectconfig.LoadDexWebConnectionsFile(t.Context(), harness.connectionsFile)
	require.NoError(t, err)
	expected, err := project.Configuration.Connections[0].ConnectionTargetFingerprint()
	require.NoError(t, err)
	require.Equal(t, expected, targetOf(), "the server uses the one sdkgo definition")

	sameTarget := base
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	sameTarget.CredentialExpiresAt = &expiresAt
	sameTarget.Credentials = testconnectors.NotesCredentials{Token: "refreshed-token"}
	sameTarget.ModulePath = "example.com/connectors/example-notes/go/v2"
	harness.writeConnectionRecords(sameTarget)
	require.Equal(t, expected, targetOf(), "a refreshed credential, its expiry, and a module move keep the target")

	for name, change := range map[string]func(*testconnectors.ConnectionsFileConnection){
		"another authorization": func(connection *testconnectors.ConnectionsFileConnection) {
			connection.AuthorizationID = "c2a9e7d4-8f3b-4e61-a2d5-5b7c9e0f1a23"
		},
		"no authorization ID": func(connection *testconnectors.ConnectionsFileConnection) { connection.AuthorizationID = "" },
		"another method set": func(connection *testconnectors.ConnectionsFileConnection) {
			connection.AuthMethodIDs = []string{"apiKey"}
		},
		"another endpoint": func(connection *testconnectors.ConnectionsFileConnection) {
			connection.Configuration = testconnectors.NotesConfig{BaseURL: "http://127.0.0.1:1"}
		},
	} {
		changed := base
		change(&changed)
		harness.writeConnectionRecords(changed)
		require.NotEqual(t, expected, targetOf(), name)
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
