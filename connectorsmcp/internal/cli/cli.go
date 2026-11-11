// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package cli implements the dex-connectors-mcp commands: serve, version, and
// list-tools.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/server"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// Exit codes of Main.
const (
	exitCodeOK            = 0
	exitCodeFailure       = 1
	exitCodeUsage         = 2
	exitCodeStateDirInUse = 3
)

// hostedProjectEnvironment lists the variables that select a hosted project.
var hostedProjectEnvironment = []string{"DEX_PROJECT_ID", "DEX_PROJECT_SCOPE", "DEX_PROJECT_CONFIG_KEY", "DEX_PROJECT_CONFIG_VERSION", "DEX_PROJECT_CONFIG_DIGEST", "DEX_PROJECT_STORAGE_BUCKET"}

// Environment is what Main reads and writes besides its arguments.
type Environment struct {
	// Stdin is watched by serve --exit-on-stdin-close.
	Stdin io.Reader
	// Stdout receives the ready line and command output.
	Stdout io.Writer
	// Stderr receives logs and human-readable errors.
	Stderr io.Writer
	// LookupEnvironment reads an environment variable.
	LookupEnvironment func(name string) (string, bool)
	// HomeDirectory returns the user's home directory.
	HomeDirectory func() (string, error)
	// LinkedConnectors lists the connectors linked into the binary.
	LinkedConnectors []registry.LinkedConnector
	// Version is the server version.
	Version string
	// ObserveProtocolPoint is for crash-injection tests; nil in production.
	ObserveProtocolPoint func(point server.ProtocolPoint, operationID string)
}

// Main runs one command and returns the process exit code.
func Main(args []string, environment *Environment) int {
	if len(args) == 0 {
		fmt.Fprint(environment.Stderr, usageText)
		return exitCodeUsage
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:], environment)
	case "version":
		return runVersion(args[1:], environment)
	case "list-tools":
		return runListTools(args[1:], environment)
	case "help", "-h", "--help":
		fmt.Fprint(environment.Stdout, usageText)
		return exitCodeOK
	default:
		fmt.Fprintf(environment.Stderr, "unknown command %q\n\n%s", args[0], usageText)
		return exitCodeUsage
	}
}

const usageText = `dex-connectors-mcp serves the agent-exposed operations of Dex connectors as MCP tools.

Commands:
  serve        serve the tools over Streamable HTTP on a loopback address
  version      print the server version and the linked connectors
  list-tools   print the tools the current connections would offer, without serving

Run "dex-connectors-mcp <command> -h" for the flags of a command.
`

// connectionSelectionFlags are the selection flags serve and list-tools share.
type connectionSelectionFlags struct {
	connections           repeatedFlag
	connectionsConfigFile string
	connectionsFile       string
}

func (selectionFlags *connectionSelectionFlags) register(flags *flag.FlagSet) {
	flags.StringVar(&selectionFlags.connectionsFile, "connections-file", "", "Dex Web connections.json (default $DEX_CONNECTOR_CONFIG_FILE, else $HOME/.dex/connectors/connections.json when it exists, else the hosted DEX_PROJECT_* project)")
	flags.Var(&selectionFlags.connections, "connection", "connectorId/connectionName[=tool,...] to enable (repeatable); default every authorized connection")
	flags.StringVar(&selectionFlags.connectionsConfigFile, "connections-config", "", "JSON file with the same selection; excludes --connection")
}

// resolve returns the tool set configuration the flags select.
func (selectionFlags *connectionSelectionFlags) resolve(environment *Environment, logger *slog.Logger) (*server.ToolSetConfig, error) {
	if len(selectionFlags.connections) > 0 && selectionFlags.connectionsConfigFile != "" {
		return nil, usageError{errors.New("--connection and --connections-config are mutually exclusive")}
	}
	linked, err := registry.New(environment.LinkedConnectors)
	if err != nil {
		return nil, fmt.Errorf("linked connectors: %w", err)
	}
	config := &server.ToolSetConfig{Registry: linked, Logger: logger}
	if len(selectionFlags.connections) > 0 {
		entries := make([]server.SelectedConnection, 0, len(selectionFlags.connections))
		for _, value := range selectionFlags.connections {
			entry, err := server.ParseConnectionFlag(value)
			if err != nil {
				return nil, usageError{err}
			}
			entries = append(entries, entry)
		}
		if config.Selection, err = server.NewConnectionSelection(entries); err != nil {
			return nil, usageError{err}
		}
	}
	if selectionFlags.connectionsConfigFile != "" {
		if config.ConnectionsConfigFile, err = filepath.Abs(selectionFlags.connectionsConfigFile); err != nil {
			return nil, usageError{err}
		}
		if _, err := server.ReadConnectionsConfigFile(config.ConnectionsConfigFile); err != nil {
			return nil, usageError{err}
		}
	}
	if config.ConnectionsFile, err = resolveConnectionsFile(selectionFlags.connectionsFile, environment); err != nil {
		return nil, err
	}
	return config, nil
}

// resolveConnectionsFile: flag, DEX_CONNECTOR_CONFIG_FILE, existing $HOME default,
// hosted (""), else the $HOME default.
func resolveConnectionsFile(flagValue string, environment *Environment) (string, error) {
	if flagValue != "" {
		return absoluteCleanPath(flagValue)
	}
	hostedVariable := ""
	for _, name := range hostedProjectEnvironment {
		if value, isSet := environment.LookupEnvironment(name); isSet && value != "" {
			hostedVariable = name
			break
		}
	}
	if path, isSet := environment.LookupEnvironment(projectconfig.LocalConnectionsFileEnvironment); isSet && path != "" {
		if hostedVariable != "" {
			return "", usageError{fmt.Errorf("%s and %s select different project configurations; set only one of them", projectconfig.LocalConnectionsFileEnvironment, hostedVariable)}
		}
		return absoluteCleanPath(path)
	}
	home, err := environment.HomeDirectory()
	if err != nil {
		return "", fmt.Errorf("find the home directory: %w", err)
	}
	homeDefault := filepath.Join(home, ".dex", "connectors", "connections.json")
	if _, err := os.Stat(homeDefault); err == nil || hostedVariable == "" {
		return homeDefault, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("inspect %s: %w", homeDefault, err)
	}
	return "", nil
}

func absoluteCleanPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	return filepath.Clean(absolute), nil
}

// usageError marks malformed flags or input; it exits with exitCodeUsage.
type usageError struct{ err error }

func (err usageError) Error() string { return err.err.Error() }

func (err usageError) Unwrap() error { return err.err }

// repeatedFlag collects every value of a repeatable flag.
type repeatedFlag []string

func (values *repeatedFlag) String() string { return strings.Join(*values, ",") }

func (values *repeatedFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func newLogger(stderr io.Writer, format string) (*slog.Logger, error) {
	switch format {
	case "text":
		return slog.New(slog.NewTextHandler(stderr, nil)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(stderr, nil)), nil
	default:
		return nil, usageError{fmt.Errorf("--log-format %q must be text or json", format)}
	}
}

func validateOutputFormat(format string) error {
	if format != "text" && format != "json" {
		return usageError{fmt.Errorf("--output %q must be text or json", format)}
	}
	return nil
}

// reportError prints err for the output format and returns exitCode.
func reportError(environment *Environment, outputFormat string, err error, exitCode int) int {
	if outputFormat == "json" {
		encoded, encodeErr := json.Marshal(struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}{Type: "error", Message: err.Error()})
		if encodeErr == nil {
			fmt.Fprintln(environment.Stdout, string(encoded))
			return exitCode
		}
	}
	fmt.Fprintln(environment.Stderr, "dex-connectors-mcp:", err)
	return exitCode
}

func exitCodeOf(err error) int {
	var usage usageError
	if errors.As(err, &usage) {
		return exitCodeUsage
	}
	return exitCodeFailure
}

// versionReport is the version --output json document.
type versionReport struct {
	Version        string                   `json:"version"`
	LaunchContract int                      `json:"launchContract"`
	GoVersion      string                   `json:"goVersion"`
	Connectors     []versionConnectorReport `json:"connectors"`
}

type versionConnectorReport struct {
	ConnectorID string   `json:"connectorId"`
	Module      string   `json:"module"`
	Version     string   `json:"version"`
	Tools       []string `json:"tools"`
}

func runVersion(args []string, environment *Environment) int {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(environment.Stderr)
	outputFormat := flags.String("output", "text", "text or json")
	if err := flags.Parse(args); err != nil {
		return exitCodeUsage
	}
	if err := validateOutputFormat(*outputFormat); err != nil {
		return reportError(environment, "text", err, exitCodeUsage)
	}
	linked, err := registry.New(environment.LinkedConnectors)
	if err != nil {
		return reportError(environment, *outputFormat, err, exitCodeFailure)
	}
	report := versionReport{Version: environment.Version, LaunchContract: server.LaunchContract, GoVersion: runtime.Version(), Connectors: []versionConnectorReport{}}
	for _, connector := range linked.Connectors() {
		tools := make([]string, 0, len(connector.ExposedAgentTools))
		for _, metadata := range connector.ExposedAgentTools {
			tools = append(tools, metadata.ToolName)
		}
		slices.Sort(tools)
		report.Connectors = append(report.Connectors, versionConnectorReport{ConnectorID: connector.ConnectorID, Module: connector.ModulePath, Version: connector.ModuleVersion, Tools: tools})
	}
	if *outputFormat == "json" {
		return writeJSONLine(environment, report)
	}
	fmt.Fprintf(environment.Stdout, "dex-connectors-mcp %s (launch contract %d, %s)\n", report.Version, report.LaunchContract, report.GoVersion)
	for _, connector := range report.Connectors {
		fmt.Fprintf(environment.Stdout, "  %s %s %s: %s\n", connector.ConnectorID, connector.Module, connector.Version, strings.Join(connector.Tools, ", "))
	}
	return exitCodeOK
}

func runListTools(args []string, environment *Environment) int {
	flags := flag.NewFlagSet("list-tools", flag.ContinueOnError)
	flags.SetOutput(environment.Stderr)
	var selectionFlags connectionSelectionFlags
	selectionFlags.register(flags)
	outputFormat := flags.String("output", "text", "text or json")
	logFormat := flags.String("log-format", "text", "text or json (stderr)")
	if err := flags.Parse(args); err != nil {
		return exitCodeUsage
	}
	if err := validateOutputFormat(*outputFormat); err != nil {
		return reportError(environment, "text", err, exitCodeUsage)
	}
	logger, err := newLogger(environment.Stderr, *logFormat)
	if err != nil {
		return reportError(environment, *outputFormat, err, exitCodeUsage)
	}
	config, err := selectionFlags.resolve(environment, logger)
	if err != nil {
		return reportError(environment, *outputFormat, err, exitCodeOf(err))
	}
	toolSet, err := server.LoadToolSet(contextForCommand(), config)
	if err != nil {
		return reportError(environment, *outputFormat, err, exitCodeFailure)
	}
	if *outputFormat == "json" {
		return writeJSONLine(environment, map[string]any{"tools": toolSet.Summaries(), "warnings": nonNilStrings(toolSet.Warnings())})
	}
	for _, summary := range toolSet.Summaries() {
		fmt.Fprintf(environment.Stdout, "%s\t%s\t%s/%s\n", summary.Name, summary.Effect, summary.ConnectorID, summary.ConnectionName)
	}
	for _, warning := range toolSet.Warnings() {
		fmt.Fprintln(environment.Stderr, "warning:", warning)
	}
	return exitCodeOK
}

func writeJSONLine(environment *Environment, value any) int {
	encoded, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintln(environment.Stderr, "dex-connectors-mcp:", err)
		return exitCodeFailure
	}
	fmt.Fprintln(environment.Stdout, string(encoded))
	return exitCodeOK
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
