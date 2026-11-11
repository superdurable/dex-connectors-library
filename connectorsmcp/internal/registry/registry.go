// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package registry lists the connector modules linked into the server binary.
//
// Release builds generate the list; tests list fake connectors.
package registry

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var connectorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

// LinkedConnector is one connector module linked into the server.
type LinkedConnector struct {
	// ConnectorID is the manifest metadata.name, such as slack.
	ConnectorID string
	// DisplayName is the manifest display name; tool titles start with it.
	DisplayName string
	// ModulePath is the connector's Go module path.
	ModulePath string
	// ModuleVersion is the linked Go module version, such as v0.26.0.
	ModuleVersion string
	// ExposedAgentTools is the connector's generated ExposedAgentTools()
	// result: the metadata of every agent.exposed operation, in manifest order.
	ExposedAgentTools []sdkgo.AgentToolMetadata
	// OpenAgentTools returns the AgentTools of one project connection.
	OpenAgentTools func(project *projectconfig.LoadedProject, connectionName string) ([]sdkgo.AgentTool, error)
}

// Registry is a validated, immutable list of linked connectors.
type Registry struct {
	connectors []LinkedConnector
}

// New validates connector IDs, module identity, and exposed metadata, then sorts the connectors.
func New(connectors []LinkedConnector) (*Registry, error) {
	seen := map[string]bool{}
	for _, connector := range connectors {
		if err := validateConnector(connector); err != nil {
			return nil, err
		}
		if seen[connector.ConnectorID] {
			return nil, fmt.Errorf("connector %s is linked twice", connector.ConnectorID)
		}
		seen[connector.ConnectorID] = true
	}
	sorted := slices.Clone(connectors)
	slices.SortFunc(sorted, func(left, right LinkedConnector) int { return strings.Compare(left.ConnectorID, right.ConnectorID) })
	return &Registry{connectors: sorted}, nil
}

// Connectors returns the linked connectors sorted by connector ID.
func (registry *Registry) Connectors() []LinkedConnector {
	return slices.Clone(registry.connectors)
}

// Connector returns the linked connector with connectorID.
func (registry *Registry) Connector(connectorID string) (LinkedConnector, bool) {
	for _, connector := range registry.connectors {
		if connector.ConnectorID == connectorID {
			return connector, true
		}
	}
	return LinkedConnector{}, false
}

// ExposedOperation returns the exposed metadata of one operation.
func (connector LinkedConnector) ExposedOperation(operationID string) (sdkgo.AgentToolMetadata, bool) {
	for _, metadata := range connector.ExposedAgentTools {
		if metadata.Operation.OperationID == operationID {
			return metadata, true
		}
	}
	return sdkgo.AgentToolMetadata{}, false
}

// OperationName is the tool name without its connector prefix, such as list_story_ids.
func OperationName(metadata sdkgo.AgentToolMetadata) string {
	return strings.TrimPrefix(metadata.ToolName, metadata.Operation.ConnectorID+"_")
}

func validateConnector(connector LinkedConnector) error {
	if !connectorIDPattern.MatchString(connector.ConnectorID) {
		return fmt.Errorf("linked connector ID %q is invalid", connector.ConnectorID)
	}
	if connector.OpenAgentTools == nil {
		return fmt.Errorf("linked connector %s has no OpenAgentTools", connector.ConnectorID)
	}
	if strings.TrimSpace(connector.ModuleVersion) == "" || strings.TrimSpace(connector.ModulePath) == "" {
		return fmt.Errorf("linked connector %s needs its module path and version", connector.ConnectorID)
	}
	operations := map[string]bool{}
	for _, metadata := range connector.ExposedAgentTools {
		if metadata.Operation.ConnectorID != connector.ConnectorID {
			return fmt.Errorf("linked connector %s exposes an operation of connector %q", connector.ConnectorID, metadata.Operation.ConnectorID)
		}
		if operations[metadata.Operation.OperationID] {
			return fmt.Errorf("linked connector %s exposes operation %s twice", connector.ConnectorID, metadata.Operation.OperationID)
		}
		operations[metadata.Operation.OperationID] = true
		prefix := connector.ConnectorID + "_"
		if !strings.HasPrefix(metadata.ToolName, prefix) || len(metadata.ToolName) == len(prefix) {
			return fmt.Errorf("linked connector %s tool name %q must start with %q", connector.ConnectorID, metadata.ToolName, prefix)
		}
		if metadata.Kind != sdkgo.AgentOperationKindQuery && metadata.Kind != sdkgo.AgentOperationKindMutation {
			return errors.New("linked connector " + connector.ConnectorID + " operation " + metadata.Operation.OperationID + " has an unknown kind")
		}
	}
	return nil
}
