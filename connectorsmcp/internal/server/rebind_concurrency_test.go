// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
)

type rpcOutcome struct {
	response rpcResponse
	err      error
}

func callInBackground(harness *testHarness, name string, arguments any, key string) chan rpcOutcome {
	outcomes := make(chan rpcOutcome, 1)
	go func() {
		response, err := postJSONRPC(harness.httpServer.URL+MCPPath, toolCallBody(name, arguments, key), nil)
		outcomes <- rpcOutcome{response: response, err: err}
	}()
	return outcomes
}

// TestARebindWhileASecondRequestWaitsForTheKeyNeverRunsTheOldBinding pins the
// old tool set in a request that waits on the key lock, then rebinds.
func TestARebindWhileASecondRequestWaitsForTheKeyNeverRunsTheOldBinding(t *testing.T) {
	arrivals := make(chan struct{}, 4)
	harness := newTestHarness(t, harnessOptions{callTimeBudget: 10500 * time.Millisecond, observe: func(point ProtocolPoint, _ string) {
		if point == ProtocolPointBeforeKeyLock {
			arrivals <- struct{}{}
		}
	}})
	harness.provider.Script("saveRecord", testconnectors.ScriptedResponse{IsHeld: true, Status: http.StatusServiceUnavailable})
	arguments := map[string]any{"name": "a", "value": "1"}

	first := callInBackground(harness, "example-notes_save_record", arguments, "flow/turn/waiting")
	<-arrivals
	require.Equal(t, "saveRecord", <-harness.provider.HeldRequests(), "the first request holds the key while its send is in flight")
	second := callInBackground(harness, "example-notes_save_record", arguments, "flow/turn/waiting")
	<-arrivals // The second request entered with the old tool set and waits for the key.

	otherAccount := testconnectors.NewFakeProvider()
	t.Cleanup(otherAccount.Close)
	harness.writeConnectionRecords(testconnectors.NotesConnection("work", otherAccount))
	harness.provider.Release()

	firstOutcome := <-first
	require.NoError(t, firstOutcome.err)
	require.Nil(t, firstOutcome.response.Error)
	var firstSettled callResult
	require.NoError(t, json.Unmarshal(firstOutcome.response.Result, &firstSettled))
	require.Equal(t, outcomeUnknown, firstSettled.outcome(), "the attempt asked to retry and its recheck found the rebind")
	require.Contains(t, firstSettled.text(), "the connection now points to a different target")

	secondOutcome := <-second
	require.NoError(t, secondOutcome.err)
	require.Nil(t, secondOutcome.response.Error)
	var settled callResult
	require.NoError(t, json.Unmarshal(secondOutcome.response.Result, &settled))
	require.Equal(t, firstSettled.text(), settled.text(), "the waiting request replays the settled answer")
	require.Equal(t, true, settled.Meta[metaKeyReplayed])
	require.Equal(t, 1, harness.provider.RecordRequests(), "the old binding never ran again")
	require.Zero(t, otherAccount.TotalRequests(), "nothing ran against the new target")
}

// TestARebindBetweenRequestEntryAndTheFirstAttemptRunsOnTheCurrentBinding
// rebinds after a new key's request pinned the old tool set.
func TestARebindBetweenRequestEntryAndTheFirstAttemptRunsOnTheCurrentBinding(t *testing.T) {
	otherAccount := testconnectors.NewFakeProvider()
	t.Cleanup(otherAccount.Close)
	var rebindOnce sync.Once
	var harness *testHarness
	harness = newTestHarness(t, harnessOptions{observe: func(point ProtocolPoint, operationID string) {
		if point == ProtocolPointBeforeKeyLock && operationID == "saveRecord" {
			rebindOnce.Do(func() {
				if err := testconnectors.WriteConnectionsFile(harness.connectionsFile, testconnectors.NotesConnection("work", otherAccount)); err != nil {
					panic(err)
				}
			})
		}
	}})
	result := harness.mustCallResult("example-notes_save_record", map[string]any{"name": "a", "value": "1"}, "flow/turn/entry-race")
	require.False(t, result.IsError, result.text())
	require.Zero(t, harness.provider.TotalRequests(), "the binding pinned at request entry never ran")
	require.Equal(t, 1, otherAccount.AppliedRecordWrites(saveRecordIdempotencyKey("flow/turn/entry-race")))

	contents, err := os.ReadFile(recordFilePath(harness.stateDirectory, "flow/turn/entry-race"))
	require.NoError(t, err)
	var record struct {
		ConnectionTarget string `json:"connectionTarget"`
	}
	require.NoError(t, json.Unmarshal(contents, &record))
	current := harness.server.CurrentToolSet(t.Context()).toolByName("example-notes_save_record")
	require.NotNil(t, current)
	require.Equal(t, current.connectionTarget, record.ConnectionTarget, "the record holds the target of the snapshot the attempt used")
}
