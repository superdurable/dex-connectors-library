// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
)

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *lockedBuffer) Write(contents []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(contents)
}

func (buffer *lockedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}

// cliFixture holds the files one test's commands share.
type cliFixture struct {
	t               *testing.T
	provider        *testconnectors.FakeProvider
	home            string
	connectionsFile string
	stateDirectory  string
	variables       map[string]string
}

func newCLIFixture(t *testing.T) *cliFixture {
	t.Helper()
	provider := testconnectors.NewFakeProvider()
	t.Cleanup(provider.Close)
	home := t.TempDir()
	fixture := &cliFixture{
		t: t, provider: provider, home: home,
		connectionsFile: filepath.Join(home, "project", "connections.json"),
		stateDirectory:  filepath.Join(home, "state"),
		variables:       map[string]string{},
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(fixture.connectionsFile), 0o700))
	require.NoError(t, testconnectors.WriteConnectionsFile(fixture.connectionsFile, testconnectors.NotesConnection("work", provider)))
	return fixture
}

func (fixture *cliFixture) environment(stdin io.Reader, stdout io.Writer, stderr io.Writer) *Environment {
	return &Environment{
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
		LookupEnvironment: func(name string) (string, bool) {
			value, isSet := fixture.variables[name]
			return value, isSet
		},
		HomeDirectory:    func() (string, error) { return fixture.home, nil },
		LinkedConnectors: []registry.LinkedConnector{testconnectors.NotesConnector()},
		Version:          "0.1.0-test",
	}
}

// runCommand runs a command that exits by itself.
func (fixture *cliFixture) runCommand(args ...string) (int, string, string) {
	var stdout, stderr lockedBuffer
	exitCode := Main(args, fixture.environment(strings.NewReader(""), &stdout, &stderr))
	return exitCode, stdout.String(), stderr.String()
}

// runningServe is a serve command in progress.
type runningServe struct {
	stdin     *io.PipeWriter
	ready     ReadyAnnouncement
	exitCodes chan int
	stderr    *lockedBuffer
}

func (fixture *cliFixture) startServe(extraArgs ...string) *runningServe {
	fixture.t.Helper()
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	serve := &runningServe{stdin: stdinWriter, exitCodes: make(chan int, 1), stderr: &lockedBuffer{}}
	args := append([]string{"serve", "--listen", "127.0.0.1:0", "--output", "json", "--exit-on-stdin-close",
		"--state-dir", fixture.stateDirectory, "--connections-file", fixture.connectionsFile}, extraArgs...)
	go func() {
		serve.exitCodes <- Main(args, fixture.environment(stdinReader, stdoutWriter, serve.stderr))
		_ = stdoutWriter.Close()
	}()
	line, err := bufio.NewReader(stdoutReader).ReadString('\n')
	require.NoError(fixture.t, err, "stderr: %s", serve.stderr.String())
	require.NoError(fixture.t, json.Unmarshal([]byte(line), &serve.ready), line)
	// Keep draining stdout so later writes never block.
	go func() { _, _ = io.Copy(io.Discard, stdoutReader) }()
	fixture.t.Cleanup(func() { _ = stdinWriter.Close() })
	return serve
}

func (serve *runningServe) stop(t *testing.T) int {
	t.Helper()
	require.NoError(t, serve.stdin.Close())
	select {
	case exitCode := <-serve.exitCodes:
		return exitCode
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not exit after stdin closed")
		return -1
	}
}

func readServerFile(t *testing.T, stateDirectory string) ReadyAnnouncement {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(stateDirectory, serverFileName))
	require.NoError(t, err)
	var announcement ReadyAnnouncement
	require.NoError(t, json.Unmarshal(contents, &announcement))
	return announcement
}

func TestServeAnnouncesReadinessServesAndStopsOnStdinClose(t *testing.T) {
	fixture := newCLIFixture(t)
	serve := fixture.startServe()
	ready := serve.ready
	require.Equal(t, "ready", ready.Type)
	require.True(t, strings.HasPrefix(ready.URL, "http://127.0.0.1:") && strings.HasSuffix(ready.URL, "/mcp"), ready.URL)
	require.Equal(t, strings.TrimSuffix(ready.URL, "/mcp")+"/healthz", ready.HealthURL)
	require.Equal(t, 1, ready.LaunchContract)
	require.Equal(t, "0.1.0-test", ready.Version)
	require.Equal(t, os.Getpid(), ready.PID)
	require.Equal(t, fixture.stateDirectory, ready.StateDir)
	require.NotNil(t, ready.ConnectionsFile)
	require.Equal(t, fixture.connectionsFile, *ready.ConnectionsFile)
	require.Nil(t, ready.ConnectionsConfigDigest)
	require.Equal(t, []string{"example-notes_get_note", "example-notes_post_note", "example-notes_save_record"}, ready.Tools)
	require.Equal(t, []string{}, ready.Warnings)
	require.Equal(t, ready, readServerFile(t, fixture.stateDirectory), "server.json is the ready line's content")
	info, err := os.Stat(filepath.Join(fixture.stateDirectory, serverFileName))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	response, err := http.Get(ready.HealthURL)
	require.NoError(t, err)
	var health map[string]any
	require.NoError(t, json.NewDecoder(response.Body).Decode(&health))
	require.NoError(t, response.Body.Close())
	require.Equal(t, ready.InstanceID, health["instanceId"])

	exitCode, stdout, _ := fixture.runCommand("serve", "--listen", "127.0.0.1:0", "--output", "json", "--state-dir", fixture.stateDirectory, "--connections-file", fixture.connectionsFile)
	require.Equal(t, exitCodeStateDirInUse, exitCode)
	require.Contains(t, stdout, `"type":"error"`)
	require.Contains(t, stdout, ready.URL, "the refusal names the running instance")

	require.NoError(t, testconnectors.WriteConnectionsFile(fixture.connectionsFile,
		testconnectors.NotesConnection("work", fixture.provider), testconnectors.NotesConnection("personal", fixture.provider)))
	require.Eventually(t, func() bool {
		if response, err := http.Get(ready.HealthURL); err == nil {
			_ = response.Body.Close()
		}
		return len(readServerFile(t, fixture.stateDirectory).Tools) == 6
	}, 10*time.Second, 20*time.Millisecond, "a reload rewrites server.json")

	require.Equal(t, exitCodeOK, serve.stop(t))
	_, err = os.Stat(filepath.Join(fixture.stateDirectory, serverFileName))
	require.ErrorIs(t, err, os.ErrNotExist, "a clean exit removes server.json")
	restarted := fixture.startServe()
	require.Equal(t, exitCodeOK, restarted.stop(t), "the instance lock was released")
}

func TestServeReportsTheSelectionDigestAndWarnings(t *testing.T) {
	fixture := newCLIFixture(t)
	configPath := filepath.Join(fixture.home, "selection.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[
		{"connection":"example-notes/work","tools":["get_note"]},{"connection":"slack/work"}]}`), 0o600))
	serve := fixture.startServe("--connections-config", configPath)
	require.NotNil(t, serve.ready.ConnectionsConfigDigest)
	require.True(t, strings.HasPrefix(*serve.ready.ConnectionsConfigDigest, "sha256:"))
	require.Equal(t, []string{"example-notes_get_note"}, serve.ready.Tools)
	require.Len(t, serve.ready.Warnings, 1)
	require.Contains(t, serve.ready.Warnings[0], "connector slack is not included")
	require.Equal(t, exitCodeOK, serve.stop(t))

	viaFlags := fixture.startServe("--connection", "slack/work", "--connection", "example-notes/work=get_note")
	require.Equal(t, *serve.ready.ConnectionsConfigDigest, *viaFlags.ready.ConnectionsConfigDigest, "flags and the file give one digest")
	require.Equal(t, exitCodeOK, viaFlags.stop(t))
}

func TestServeUsageErrors(t *testing.T) {
	fixture := newCLIFixture(t)
	emptyToolsConfig := filepath.Join(fixture.home, "empty-tools.json")
	require.NoError(t, os.WriteFile(emptyToolsConfig, []byte(`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[{"connection":"example-notes/work","tools":[]}]}`), 0o600))
	trailingConfig := filepath.Join(fixture.home, "trailing.json")
	require.NoError(t, os.WriteFile(trailingConfig, []byte(`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[]} {}`), 0o600))
	cases := map[string]struct {
		args        []string
		messagePart string
	}{
		"non-loopback listen":       {[]string{"--listen", "0.0.0.0:8803"}, "loopback"},
		"both selection forms":      {[]string{"--connection", "example-notes/work", "--connections-config", "x.json"}, "mutually exclusive"},
		"malformed connection":      {[]string{"--connection", "example-notes"}, "example-notes"},
		"short retention":           {[]string{"--call-state-retention", "1h"}, "retention"},
		"unknown log format":        {[]string{"--log-format", "xml"}, "log-format"},
		"empty flag tool list":      {[]string{"--connection", "example-notes/work="}, "example-notes/work="},
		"empty config tool list":    {[]string{"--connections-config", emptyToolsConfig}, "connection example-notes/work lists no tools"},
		"config with trailing data": {[]string{"--connections-config", trailingConfig}, "data after its JSON document"},
	}
	for name, testCase := range cases {
		args := append([]string{"serve", "--output", "json", "--state-dir", fixture.stateDirectory, "--connections-file", fixture.connectionsFile}, testCase.args...)
		exitCode, stdout, _ := fixture.runCommand(args...)
		require.Equal(t, exitCodeUsage, exitCode, name)
		var report map[string]string
		require.NoError(t, json.Unmarshal([]byte(stdout), &report), name)
		require.Equal(t, "error", report["type"], name)
		require.Contains(t, report["message"], testCase.messagePart, name)
	}
}

func TestServeRefusesUnsafeOrForeignStateDirectories(t *testing.T) {
	fixture := newCLIFixture(t)
	shared := filepath.Join(fixture.home, "shared")
	require.NoError(t, os.Mkdir(shared, 0o700))
	require.NoError(t, os.Chmod(shared, 0o777))
	exitCode, stdout, _ := fixture.runCommand("serve", "--output", "json", "--listen", "127.0.0.1:0", "--state-dir", shared, "--connections-file", fixture.connectionsFile)
	require.Equal(t, exitCodeFailure, exitCode)
	require.Contains(t, stdout, "writable by group or others")

	newer := filepath.Join(fixture.home, "newer")
	require.NoError(t, os.MkdirAll(filepath.Join(newer, "calls", "v1"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(newer, "calls", "v1", "FORMAT"), []byte("dex-connectors-mcp/call-state/v2\n"), 0o600))
	exitCode, stdout, _ = fixture.runCommand("serve", "--output", "json", "--listen", "127.0.0.1:0", "--state-dir", newer, "--connections-file", fixture.connectionsFile)
	require.Equal(t, exitCodeFailure, exitCode)
	require.Contains(t, stdout, "call-state/v2")
}

func TestConnectionsFileResolutionOrder(t *testing.T) {
	fixture := newCLIFixture(t)
	resolve := func(flagValue string) (string, error) {
		return resolveConnectionsFile(flagValue, fixture.environment(nil, io.Discard, io.Discard))
	}
	homeDefault := filepath.Join(fixture.home, ".dex", "connectors", "connections.json")

	path, err := resolve("")
	require.NoError(t, err)
	require.Equal(t, homeDefault, path, "with nothing configured, the $HOME default is watched")

	fixture.variables["DEX_PROJECT_ID"] = "p-1"
	path, err = resolve("")
	require.NoError(t, err)
	require.Equal(t, "", path, "hosted when the $HOME default does not exist")

	require.NoError(t, os.MkdirAll(filepath.Dir(homeDefault), 0o700))
	require.NoError(t, os.WriteFile(homeDefault, []byte("{}"), 0o600))
	path, err = resolve("")
	require.NoError(t, err)
	require.Equal(t, homeDefault, path, "the $HOME default wins over hosted")

	fixture.variables["DEX_CONNECTOR_CONFIG_FILE"] = "relative/connections.json"
	_, err = resolve("")
	require.ErrorContains(t, err, "set only one of them")
	delete(fixture.variables, "DEX_PROJECT_ID")
	path, err = resolve("")
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(path) && strings.HasSuffix(path, "relative/connections.json"))

	path, err = resolve("./flag/../flag/connections.json")
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(path) && strings.HasSuffix(path, "/flag/connections.json"), "the flag wins and is cleaned")
}

func TestVersionAndListToolsJSON(t *testing.T) {
	fixture := newCLIFixture(t)
	exitCode, stdout, _ := fixture.runCommand("version", "--output", "json")
	require.Equal(t, exitCodeOK, exitCode)
	var version versionReport
	require.NoError(t, json.Unmarshal([]byte(stdout), &version))
	require.Equal(t, "0.1.0-test", version.Version)
	require.Equal(t, 1, version.LaunchContract)
	require.Equal(t, []versionConnectorReport{{
		ConnectorID: testconnectors.NotesConnectorID, Module: testconnectors.NotesModulePath, Version: testconnectors.NotesModuleVersion,
		Tools: []string{"example-notes_get_note", "example-notes_post_note", "example-notes_save_record"},
	}}, version.Connectors)

	exitCode, stdout, _ = fixture.runCommand("list-tools", "--output", "json", "--connections-file", fixture.connectionsFile, "--connection", "example-notes/work=postNote")
	require.Equal(t, exitCodeOK, exitCode)
	var listed struct {
		Tools []struct {
			Name                  string `json:"name"`
			Effect                string `json:"effect"`
			IsApprovalRecommended bool   `json:"approvalRecommended"`
		} `json:"tools"`
		Warnings []string `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &listed))
	require.Len(t, listed.Tools, 1)
	require.Equal(t, "example-notes_post_note", listed.Tools[0].Name)
	require.Equal(t, "non_idempotent_write", listed.Tools[0].Effect)
	require.True(t, listed.Tools[0].IsApprovalRecommended)
	require.Empty(t, listed.Warnings)

	exitCode, _, stderr := fixture.runCommand("unknown-command")
	require.Equal(t, exitCodeUsage, exitCode)
	require.Contains(t, stderr, "unknown command")
}
