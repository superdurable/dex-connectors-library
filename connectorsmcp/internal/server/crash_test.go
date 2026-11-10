// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//go:build unix

package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/callstate"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
)

const (
	crashHelperEnvironment          = "CONNECTORSMCP_CRASH_HELPER"
	crashHelperStateEnvironment     = "CONNECTORSMCP_CRASH_STATE_DIR"
	crashHelperConnectionsEnv       = "CONNECTORSMCP_CRASH_CONNECTIONS_FILE"
	crashHelperPointEnvironment     = "CONNECTORSMCP_CRASH_AT"
	crashHelperOperationEnvironment = "CONNECTORSMCP_CRASH_OPERATION"
)

func TestMain(m *testing.M) {
	if os.Getenv(crashHelperEnvironment) == "1" {
		runCrashHelper()
		return
	}
	os.Exit(m.Run())
}

// runCrashHelper serves the notes connector and kills itself with SIGKILL at
// the configured protocol point, as a power loss or kill -9 would.
func runCrashHelper() {
	crashPoint := ProtocolPoint(os.Getenv(crashHelperPointEnvironment))
	crashOperation := os.Getenv(crashHelperOperationEnvironment)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	linked, err := registry.New([]registry.LinkedConnector{testconnectors.NotesConnector()})
	exitOnError(err)
	store, err := callstate.Open(os.Getenv(crashHelperStateEnvironment), time.Now)
	exitOnError(err)
	server, err := New(context.Background(), &Config{
		Version: "crash-helper", InstanceID: "crash-helper",
		ToolSet:        ToolSetConfig{Registry: linked, ConnectionsFile: os.Getenv(crashHelperConnectionsEnv), Logger: logger},
		Store:          store,
		CallTimeBudget: 5 * time.Minute, CallStateRetention: 720 * time.Hour, MaxResultBytes: 65536, Clock: SystemClock{},
		ObserveProtocolPoint: func(point ProtocolPoint, operationID string) {
			if point == crashPoint && operationID == crashOperation {
				exitOnError(syscall.Kill(os.Getpid(), syscall.SIGKILL))
			}
		},
	})
	exitOnError(err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	exitOnError(err)
	fmt.Printf("ready http://%s%s\n", listener.Addr(), MCPPath)
	exitOnError(http.Serve(listener, server.Handler()))
}

func exitOnError(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// crashFixture is a fake provider and the files several server processes share.
type crashFixture struct {
	t               *testing.T
	provider        *testconnectors.FakeProvider
	stateDirectory  string
	connectionsFile string
}

func newCrashFixture(t *testing.T) *crashFixture {
	t.Helper()
	provider := testconnectors.NewFakeProvider()
	t.Cleanup(provider.Close)
	directory := t.TempDir()
	fixture := &crashFixture{t: t, provider: provider, stateDirectory: filepath.Join(directory, "state"), connectionsFile: filepath.Join(directory, "connections.json")}
	require.NoError(t, testconnectors.WriteConnectionsFile(fixture.connectionsFile, testconnectors.NotesConnection("work", provider)))
	return fixture
}

// crashHelper is one server process.
type crashHelper struct {
	command  *exec.Cmd
	endpoint string
}

// startHelper starts a server process that kills itself at crashPoint of operationID.
func (fixture *crashFixture) startHelper(crashPoint ProtocolPoint, operationID string) *crashHelper {
	fixture.t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^$")
	command.Env = append(os.Environ(),
		crashHelperEnvironment+"=1",
		crashHelperStateEnvironment+"="+fixture.stateDirectory,
		crashHelperConnectionsEnv+"="+fixture.connectionsFile,
		crashHelperPointEnvironment+"="+string(crashPoint),
		crashHelperOperationEnvironment+"="+operationID,
	)
	command.Stderr = os.Stderr
	stdout, err := command.StdoutPipe()
	require.NoError(fixture.t, err)
	require.NoError(fixture.t, command.Start())
	helper := &crashHelper{command: command}
	fixture.t.Cleanup(func() { helper.kill() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(fixture.t, err)
	helper.endpoint = strings.TrimSpace(strings.TrimPrefix(line, "ready "))
	return helper
}

func (helper *crashHelper) kill() {
	_ = helper.command.Process.Signal(syscall.SIGKILL)
	_ = helper.command.Wait()
}

// waitForExit waits for a helper that kills itself.
func (helper *crashHelper) waitForExit(t *testing.T) {
	t.Helper()
	err := helper.command.Wait()
	var exitError *exec.ExitError
	require.ErrorAs(t, err, &exitError)
	status, isWaitStatus := exitError.Sys().(syscall.WaitStatus)
	require.True(t, isWaitStatus)
	require.Equal(t, syscall.SIGKILL, status.Signal(), "the helper died by SIGKILL")
}

func (helper *crashHelper) call(name string, arguments any, key string) (rpcResponse, error) {
	return postJSONRPC(helper.endpoint, toolCallBody(name, arguments, key), nil)
}

func decodeCallResult(t *testing.T, response rpcResponse) callResult {
	t.Helper()
	require.Nil(t, response.Error, "unexpected JSON-RPC error")
	var result callResult
	require.NoError(t, json.Unmarshal(response.Result, &result))
	return result
}

type crashScenario struct {
	name       string
	crashPoint ProtocolPoint
	// isExternalKill holds the provider request and kills the helper from the test.
	isExternalKill bool
	// isEffectApplied lets the held provider act before the kill.
	isEffectApplied bool
	expectOutcome   any
	expectReplayed  bool
}

// TestNonIdempotentWriteCrashPointsNeverSendTwice kills the server at every
// point of the protocol and re-issues the same key in a new process.
func TestNonIdempotentWriteCrashPointsNeverSendTwice(t *testing.T) {
	scenarios := []crashScenario{
		{name: "before the record exists", crashPoint: ProtocolPointBeforeRecordCreate, expectOutcome: nil},
		{name: "after the record, before the claim", crashPoint: ProtocolPointAfterRecordCreate, expectOutcome: outcomeUnknown},
		{name: "during the send", isExternalKill: true, expectOutcome: outcomeUnknown},
		{name: "after the provider acted, before its answer", isExternalKill: true, isEffectApplied: true, expectOutcome: outcomeUnknown},
		{name: "after the send, before the result is stored", crashPoint: ProtocolPointAfterAttempt, expectOutcome: outcomeUnknown},
		{name: "after the result is stored", crashPoint: ProtocolPointAfterResultStored, expectOutcome: nil, expectReplayed: true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newCrashFixture(t)
			arguments := map[string]any{"text": "hello"}
			fixture.crashDuringCall(scenario, "postNote", "example-notes_post_note", arguments, "flow/turn/crash")
			survivor := fixture.startHelper("", "")
			first := decodeCallResult(t, mustCall(t, survivor, "example-notes_post_note", arguments, "flow/turn/crash"))
			second := decodeCallResult(t, mustCall(t, survivor, "example-notes_post_note", arguments, "flow/turn/crash"))
			require.Equal(t, scenario.expectOutcome, first.outcome())
			require.Equal(t, scenario.expectReplayed, first.Meta[metaKeyReplayed] == true)
			require.Equal(t, first.text(), second.text(), "the re-issue's answer is final")
			require.Equal(t, true, second.Meta[metaKeyReplayed])
			require.LessOrEqual(t, fixture.provider.NoteRequests(), 1, "at most one send per key")
			require.LessOrEqual(t, fixture.provider.CreatedNotes(), 1)
			if scenario.expectOutcome == nil {
				require.Equal(t, 1, fixture.provider.CreatedNotes())
			}
		})
	}
}

// TestIdempotentWriteCrashPointsApplyAtMostOnce kills the server at every
// point and re-issues the key; read-back and the provider key keep one effect.
func TestIdempotentWriteCrashPointsApplyAtMostOnce(t *testing.T) {
	scenarios := []crashScenario{
		{name: "before the record exists", crashPoint: ProtocolPointBeforeRecordCreate},
		{name: "after the record, before the request", crashPoint: ProtocolPointAfterRecordCreate},
		{name: "during the send", isExternalKill: true},
		{name: "after the provider acted, before its answer", isExternalKill: true, isEffectApplied: true},
		{name: "after the send, before the result is stored", crashPoint: ProtocolPointAfterAttempt},
		{name: "after the result is stored", crashPoint: ProtocolPointAfterResultStored, expectReplayed: true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newCrashFixture(t)
			arguments := map[string]any{"name": "a", "value": "1"}
			fixture.crashDuringCall(scenario, "saveRecord", "example-notes_save_record", arguments, "flow/turn/crash")
			survivor := fixture.startHelper("", "")
			result := decodeCallResult(t, mustCall(t, survivor, "example-notes_save_record", arguments, "flow/turn/crash"))
			require.False(t, result.IsError, result.text())
			require.JSONEq(t, `{"value":"1"}`, result.text())
			require.Equal(t, scenario.expectReplayed, result.Meta[metaKeyReplayed] == true)
			require.Equal(t, 1, fixture.provider.MaximumAppliedRecordWrites(), "exactly one effect per key")
		})
	}
}

// TestAnUnobservedEffectSurvivesACrashBeforeItIsRecorded kills the server
// after an attempt that applied but asked to retry; the resumed call must not
// end rejected.
func TestAnUnobservedEffectSurvivesACrashBeforeItIsRecorded(t *testing.T) {
	fixture := newCrashFixture(t)
	fixture.provider.Script("saveRecord",
		testconnectors.ScriptedResponse{IsEffectAppliedFirst: true, Status: http.StatusServiceUnavailable},
		testconnectors.ScriptedResponse{Status: http.StatusBadRequest},
	)
	fixture.provider.Script("readRecord", testconnectors.ScriptedResponse{Status: http.StatusNotFound})
	arguments := map[string]any{"name": "a", "value": "1"}
	fixture.crashDuringCall(crashScenario{crashPoint: ProtocolPointAfterAttempt}, "saveRecord", "example-notes_save_record", arguments, "flow/turn/unobserved")
	survivor := fixture.startHelper("", "")
	result := decodeCallResult(t, mustCall(t, survivor, "example-notes_save_record", arguments, "flow/turn/unobserved"))
	require.Equal(t, outcomeUnknown, result.outcome(), "the crashed attempt's effect was never observed, so the later rejection proves nothing")
	require.Equal(t, 1, fixture.provider.AppliedRecordWrites(saveRecordIdempotencyKey("flow/turn/unobserved")))
}

// crashDuringCall issues one call to a helper that dies during it.
func (fixture *crashFixture) crashDuringCall(scenario crashScenario, operationID string, toolName string, arguments any, key string) {
	t := fixture.t
	t.Helper()
	if scenario.isExternalKill {
		if scenario.isEffectApplied {
			fixture.provider.Script(operationID, testconnectors.ScriptedResponse{IsHeldAfterEffect: true})
		} else {
			fixture.provider.Script(operationID, testconnectors.ScriptedResponse{IsHeld: true})
		}
		victim := fixture.startHelper("", "")
		answered := make(chan error, 1)
		go func() {
			_, err := victim.call(toolName, arguments, key)
			answered <- err
		}()
		require.Equal(t, operationID, <-fixture.provider.HeldRequests())
		victim.kill()
		require.Error(t, <-answered, "the connection broke with the process")
		return
	}
	victim := fixture.startHelper(scenario.crashPoint, operationID)
	_, err := victim.call(toolName, arguments, key)
	require.Error(t, err, "the helper died before answering")
	victim.waitForExit(t)
}

func mustCall(t *testing.T, helper *crashHelper, name string, arguments any, key string) rpcResponse {
	t.Helper()
	response, err := helper.call(name, arguments, key)
	require.NoError(t, err)
	return response
}

// TestSameKeyCallsAcrossTwoProcessesSendOnce holds the first send so the
// second process waits on the key's file lock, then replays.
func TestSameKeyCallsAcrossTwoProcessesSendOnce(t *testing.T) {
	fixture := newCrashFixture(t)
	first := fixture.startHelper("", "")
	second := fixture.startHelper("", "")
	fixture.provider.Script("postNote", testconnectors.ScriptedResponse{IsHeld: true})
	arguments := map[string]any{"text": "hello"}
	answers := make(chan rpcResponse, 2)
	go func() { answers <- mustCall(t, first, "example-notes_post_note", arguments, "flow/turn/shared") }()
	require.Equal(t, "postNote", <-fixture.provider.HeldRequests())
	go func() { answers <- mustCall(t, second, "example-notes_post_note", arguments, "flow/turn/shared") }()
	fixture.provider.Release()
	results := []callResult{decodeCallResult(t, <-answers), decodeCallResult(t, <-answers)}
	require.Equal(t, results[0].text(), results[1].text())
	replayedCount := 0
	for _, result := range results {
		require.False(t, result.IsError)
		if result.Meta[metaKeyReplayed] == true {
			replayedCount++
		}
	}
	require.Equal(t, 1, replayedCount)
	require.Equal(t, 1, fixture.provider.NoteRequests())
}

func TestSameKeyCallsAcrossGoroutinesSendOnce(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	arguments := map[string]any{"text": "hello"}
	var wait sync.WaitGroup
	results := make([]callResult, 8)
	for index := range results {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, err := postJSONRPC(harness.httpServer.URL+MCPPath, toolCallBody("example-notes_post_note", arguments, "flow/turn/concurrent"), nil)
			if err != nil {
				panic(err)
			}
			if err := json.Unmarshal(response.Result, &results[index]); err != nil {
				panic(err)
			}
		}()
	}
	wait.Wait()
	for _, result := range results {
		require.False(t, result.IsError)
		require.Equal(t, results[0].text(), result.text())
	}
	require.Equal(t, 1, harness.provider.NoteRequests())
}

func TestUntrustworthyRecordsAnswerUnknownWithoutSending(t *testing.T) {
	for name, contents := range map[string]string{
		"corrupt":        `{"schemaVersion":"dex-connectors-mcp/call-state/v1",`,
		"unknown schema": `{"schemaVersion":"dex-connectors-mcp/call-state/v9","status":"completed"}`,
	} {
		t.Run(name, func(t *testing.T) {
			harness := newTestHarness(t, harnessOptions{})
			stateKey := callstate.StateKey("flow/turn/corrupt")
			shard := filepath.Join(harness.stateDirectory, "calls", "v1", stateKey[:2])
			require.NoError(t, os.MkdirAll(shard, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(shard, stateKey+".json"), []byte(contents), 0o600))
			for _, tool := range []string{"example-notes_post_note", "example-notes_save_record"} {
				result := harness.mustCallResult(tool, map[string]any{"text": "a", "name": "a", "value": "b"}, "flow/turn/corrupt")
				require.Equal(t, outcomeUnknown, result.outcome(), tool)
			}
			require.Equal(t, 0, harness.provider.NoteRequests())
			require.Equal(t, 0, harness.provider.RecordRequests())
		})
	}
}

func TestReadOnlyStateDirectoryNeverSendsAWrite(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	callsDirectory := filepath.Join(harness.stateDirectory, "calls", "v1")
	require.NoError(t, os.Chmod(callsDirectory, 0o500))
	t.Cleanup(func() { _ = os.Chmod(callsDirectory, 0o700) })

	rejected := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "a"}, "flow/turn/read-only")
	require.Equal(t, outcomeRejected, rejected.outcome(), "no record can exist, so nothing was sent")
	interrupted := harness.callTool("example-notes_save_record", map[string]any{"name": "a", "value": "b"}, "flow/turn/read-only-2")
	require.NotNil(t, interrupted.Error)
	read := harness.mustCallResult("example-notes_get_note", map[string]any{"noteId": "welcome"}, "flow/turn/read-only-3")
	require.False(t, read.IsError, "a read keeps no state and still runs")
	require.Equal(t, 0, harness.provider.NoteRequests())
	require.Equal(t, 0, harness.provider.RecordRequests())
}
