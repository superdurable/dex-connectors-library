// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command dex-connectors-mcp serves the agent-exposed operations of the linked
// Dex connectors as MCP tools. See connectorsmcp/README.md.
package main

import (
	"os"
	"runtime/debug"

	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/cli"
)

// version is set with -ldflags "-X main.version=..." by the release build.
var version = ""

func main() {
	os.Exit(cli.Main(os.Args[1:], &cli.Environment{
		Stdin:             os.Stdin,
		Stdout:            os.Stdout,
		Stderr:            os.Stderr,
		LookupEnvironment: os.LookupEnv,
		HomeDirectory:     os.UserHomeDir,
		LinkedConnectors:  linkedConnectors(),
		Version:           serverVersion(),
	}))
}

// serverVersion is the -ldflags version, else the module version that go
// install recorded, else "devel".
func serverVersion() string {
	if version != "" {
		return version
	}
	if info, isAvailable := debug.ReadBuildInfo(); isAvailable && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "devel"
}
