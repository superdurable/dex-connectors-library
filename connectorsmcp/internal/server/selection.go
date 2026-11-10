// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/canonicaljson"
)

// ConnectionsConfigSchemaVersion is the schemaVersion of a --connections-config file.
const ConnectionsConfigSchemaVersion = "dex-connectors-mcp/connections-config/v1"

var selectionConnectorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

// SelectedConnection enables one connection, optionally narrowed to tools.
type SelectedConnection struct {
	// ConnectorID is the manifest connector ID.
	ConnectorID string
	// ConnectionName is the Dex Web connection name.
	ConnectionName string
	// Tools are snake-case operation names or operationIds; nil offers every exposed tool.
	Tools []string
}

// ConnectionSelection is the enabled connections; nil means every authorized connection.
type ConnectionSelection struct {
	connections []SelectedConnection
	warnings    []string
}

// NewConnectionSelection refuses repeated connections and empty tool lists;
// it de-duplicates repeated tools with a warning.
func NewConnectionSelection(entries []SelectedConnection) (*ConnectionSelection, error) {
	selection := &ConnectionSelection{}
	seen := map[string]bool{}
	for _, entry := range entries {
		if !selectionConnectorIDPattern.MatchString(entry.ConnectorID) {
			return nil, fmt.Errorf("connection %s/%s: connector ID %q is invalid", entry.ConnectorID, entry.ConnectionName, entry.ConnectorID)
		}
		if strings.TrimSpace(entry.ConnectionName) == "" {
			return nil, fmt.Errorf("connection %s/: the connection name is empty", entry.ConnectorID)
		}
		label := entry.ConnectorID + "/" + entry.ConnectionName
		if seen[label] {
			return nil, fmt.Errorf("connection %s is listed twice", label)
		}
		seen[label] = true
		if entry.Tools != nil && len(entry.Tools) == 0 {
			return nil, fmt.Errorf("connection %s lists no tools; omit the list to offer every exposed tool", label)
		}
		if entry.Tools != nil {
			tools := make([]string, 0, len(entry.Tools))
			for _, tool := range entry.Tools {
				if strings.TrimSpace(tool) == "" {
					return nil, fmt.Errorf("connection %s lists an empty tool", label)
				}
				if slices.Contains(tools, tool) {
					selection.warnings = append(selection.warnings, fmt.Sprintf("connection %s lists tool %s twice; it is kept once", label, tool))
					continue
				}
				tools = append(tools, tool)
			}
			slices.Sort(tools)
			entry.Tools = tools
		}
		selection.connections = append(selection.connections, entry)
	}
	slices.SortFunc(selection.connections, func(left, right SelectedConnection) int {
		return strings.Compare(left.ConnectorID+"/"+left.ConnectionName, right.ConnectorID+"/"+right.ConnectionName)
	})
	return selection, nil
}

// ParseConnectionFlag splits at the first slash, the first equals sign, then
// commas; an equals sign with no tools is an error.
func ParseConnectionFlag(value string) (SelectedConnection, error) {
	connectorID, rest, hasSlash := strings.Cut(value, "/")
	if !hasSlash {
		return SelectedConnection{}, fmt.Errorf("--connection %q must be connectorId/connectionName[=tool,...]", value)
	}
	connectionName, toolList, hasTools := strings.Cut(rest, "=")
	if strings.Contains(connectionName, ",") {
		return SelectedConnection{}, fmt.Errorf("--connection %q: a connection name with a comma needs --connections-config", value)
	}
	entry := SelectedConnection{ConnectorID: connectorID, ConnectionName: connectionName}
	if hasTools && toolList == "" {
		return SelectedConnection{}, fmt.Errorf("--connection %q lists no tools after '='; omit '=' to offer every exposed tool", value)
	}
	if hasTools {
		for _, tool := range strings.Split(toolList, ",") {
			if strings.TrimSpace(tool) == "" {
				return SelectedConnection{}, fmt.Errorf("--connection %q lists an empty tool", value)
			}
			entry.Tools = append(entry.Tools, tool)
		}
	}
	return entry, nil
}

// ReadConnectionsConfigFile reads a --connections-config file.
func ReadConnectionsConfigFile(path string) (*ConnectionSelection, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read connections config: %w", err)
	}
	return ParseConnectionsConfig(contents)
}

// ParseConnectionsConfig parses one --connections-config document; only an absent tools list means all.
func ParseConnectionsConfig(contents []byte) (*ConnectionSelection, error) {
	var document struct {
		SchemaVersion string `json:"schemaVersion"`
		Connections   []struct {
			Connection string   `json:"connection"`
			Tools      []string `json:"tools"`
		} `json:"connections"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, errors.New("connections config is not a valid connections config document")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("connections config has data after its JSON document")
	}
	if document.SchemaVersion != ConnectionsConfigSchemaVersion {
		return nil, fmt.Errorf("connections config schemaVersion %q is not %q", document.SchemaVersion, ConnectionsConfigSchemaVersion)
	}
	entries := make([]SelectedConnection, 0, len(document.Connections))
	for _, connection := range document.Connections {
		connectorID, connectionName, hasSlash := strings.Cut(connection.Connection, "/")
		if !hasSlash {
			return nil, fmt.Errorf("connections config entry %q must be connectorId/connectionName", connection.Connection)
		}
		entries = append(entries, SelectedConnection{ConnectorID: connectorID, ConnectionName: connectionName, Tools: connection.Tools})
	}
	return NewConnectionSelection(entries)
}

// Connections returns the selected connections sorted by connectorId/connectionName.
func (selection *ConnectionSelection) Connections() []SelectedConnection {
	if selection == nil {
		return nil
	}
	return slices.Clone(selection.connections)
}

// Warnings returns the selection's non-fatal problems.
func (selection *ConnectionSelection) Warnings() []string {
	if selection == nil {
		return nil
	}
	return slices.Clone(selection.warnings)
}

// ConnectionsConfigDigest is the connectionsConfigDigest from the README, or nil
// without a selection.
func (selection *ConnectionSelection) ConnectionsConfigDigest() *string {
	if selection == nil {
		return nil
	}
	type selectionDigestEntry struct {
		Connection string   `json:"connection"`
		Tools      []string `json:"tools"`
	}
	entries := make([]selectionDigestEntry, 0, len(selection.connections))
	for _, connection := range selection.connections {
		entries = append(entries, selectionDigestEntry{Connection: connection.ConnectorID + "/" + connection.ConnectionName, Tools: connection.Tools})
	}
	slices.SortFunc(entries, func(left, right selectionDigestEntry) int { return strings.Compare(left.Connection, right.Connection) })
	canonical, err := canonicaljson.Marshal(map[string]any{"connections": entries})
	if err != nil {
		panic(fmt.Sprintf("encode connection selection: %v", err))
	}
	digest := "sha256:" + canonicaljson.SHA256Hex(canonical)
	return &digest
}
