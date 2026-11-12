// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command notes_fixture_server is a test fixture: "serve" with a fake write connector and provider.
//
//	notes_fixture_server --provider-stats-file PATH -- [serve flags]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/cli"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
)

// notesConnectionName is the Dex Web connection name of the fake connector.
const notesConnectionName = "notes"

// providerStats is the stats file: what reached the fake provider.
type providerStats struct {
	// RecordRequests counts saveRecord requests that reached the provider.
	RecordRequests int `json:"recordRequests"`
	// MaximumAppliedRecordWrites is the highest applied count of any provider key.
	MaximumAppliedRecordWrites int `json:"maximumAppliedRecordWrites"`
}

func main() {
	os.Exit(runNotesFixtureServer(os.Args[1:]))
}

func runNotesFixtureServer(args []string) int {
	flags := flag.NewFlagSet("notes_fixture_server", flag.ContinueOnError)
	statsPath := flags.String("provider-stats-file", "", "where to write the fake provider's request counts on exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *statsPath == "" {
		fmt.Fprintln(os.Stderr, "usage: notes_fixture_server --provider-stats-file PATH -- [serve flags]")
		return 2
	}
	provider := testconnectors.NewFakeProvider()
	defer provider.Close()
	connectionsDirectory, err := os.MkdirTemp("", "notes-fixture-connections-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create the connections directory:", err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(connectionsDirectory); err != nil {
			fmt.Fprintln(os.Stderr, "remove the connections directory:", err)
		}
	}()
	connectionsFile := filepath.Join(connectionsDirectory, "connections.json")
	if err := testconnectors.WriteConnectionsFile(connectionsFile, testconnectors.NotesConnection(notesConnectionName, provider)); err != nil {
		fmt.Fprintln(os.Stderr, "write the connections file:", err)
		return 1
	}
	serveArguments := append([]string{"serve", "--connections-file", connectionsFile}, flags.Args()...)
	exitCode := cli.Main(serveArguments, &cli.Environment{
		Stdin:             os.Stdin,
		Stdout:            os.Stdout,
		Stderr:            os.Stderr,
		LookupEnvironment: os.LookupEnv,
		HomeDirectory:     os.UserHomeDir,
		LinkedConnectors:  []registry.LinkedConnector{testconnectors.NotesConnector()},
		Version:           "notes-fixture",
	})
	if err := writeProviderStats(*statsPath, provider); err != nil {
		fmt.Fprintln(os.Stderr, "write the provider stats:", err)
		return 1
	}
	return exitCode
}

func writeProviderStats(path string, provider *testconnectors.FakeProvider) error {
	contents, err := json.Marshal(providerStats{
		RecordRequests:             provider.RecordRequests(),
		MaximumAppliedRecordWrites: provider.MaximumAppliedRecordWrites(),
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, contents, 0o600)
}
