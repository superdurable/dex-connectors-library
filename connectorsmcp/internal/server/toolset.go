// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/callstate"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/canonicaljson"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// defaultAttemptTimeout bounds an attempt whose operation sets no Execute timeout.
const defaultAttemptTimeout = 60 * time.Second

// ToolSet is immutable; in-flight calls keep the set they started with.
type ToolSet struct {
	registry        *registry.Registry
	projectIdentity string
	tools           []*toolBinding
	byName          map[string]*toolBinding
	byIdentity      map[toolIdentity]*toolBinding
	warnings        []string
	digest          string
	mcpServer       *mcp.Server
}

// toolIdentity names a tool independently of its name.
type toolIdentity struct {
	connectorID    string
	operationID    string
	connectionName string
}

// toolBinding is one offered tool: an exposed operation bound to one connection.
type toolBinding struct {
	name           string
	tool           sdkgo.AgentTool
	metadata       sdkgo.AgentToolMetadata
	connector      registry.LinkedConnector
	connectionName string
	effect         sdkgo.AgentToolEffect
	inputSchema    *jsonschema.Resolved
	// connectionTarget is connectionTargetDigest of the connection's configuration.
	connectionTarget string
	mcpTool          *mcp.Tool
}

// ToolNames returns the offered tool names, sorted.
func (toolSet *ToolSet) ToolNames() []string {
	names := make([]string, 0, len(toolSet.tools))
	for _, binding := range toolSet.tools {
		names = append(names, binding.name)
	}
	return names
}

// Warnings returns the non-fatal problems found while building the set.
func (toolSet *ToolSet) Warnings() []string { return slices.Clone(toolSet.warnings) }

// Digest identifies the offered tools and their metadata.
func (toolSet *ToolSet) Digest() string { return toolSet.digest }

// ProjectIdentity names the project configuration the set was built from.
func (toolSet *ToolSet) ProjectIdentity() string { return toolSet.projectIdentity }

// ToolSummary describes one offered tool for list-tools.
type ToolSummary struct {
	// Name is the MCP tool name.
	Name string `json:"name"`
	// ConnectorID is the manifest connector ID.
	ConnectorID string `json:"connector"`
	// ConnectionName is the Dex Web connection name.
	ConnectionName string `json:"connection"`
	// OperationID is the manifest operation ID.
	OperationID string `json:"operation"`
	// Effect is read, idempotent_write, or non_idempotent_write.
	Effect string `json:"effect"`
	// IsApprovalRecommended reports agent.approval: recommended.
	IsApprovalRecommended bool `json:"approvalRecommended"`
	// Description is the text the model reads.
	Description string `json:"description"`
}

// Summaries returns one ToolSummary per offered tool, sorted by name.
func (toolSet *ToolSet) Summaries() []ToolSummary {
	summaries := make([]ToolSummary, 0, len(toolSet.tools))
	for _, binding := range toolSet.tools {
		summaries = append(summaries, ToolSummary{
			Name: binding.name, ConnectorID: binding.connector.ConnectorID, ConnectionName: binding.connectionName,
			OperationID: binding.metadata.Operation.OperationID, Effect: string(binding.effect),
			IsApprovalRecommended: binding.metadata.IsApprovalRecommended, Description: binding.metadata.Description,
		})
	}
	return summaries
}

func (toolSet *ToolSet) toolByName(name string) *toolBinding { return toolSet.byName[name] }

// toolForRequest returns the offered tool a call-state request names, or nil.
func (toolSet *ToolSet) toolForRequest(request callstate.Request) *toolBinding {
	if request.ProjectIdentity != toolSet.projectIdentity {
		return nil
	}
	return toolSet.byIdentity[toolIdentity{connectorID: request.ConnectorID, operationID: request.OperationID, connectionName: request.ConnectionName}]
}

// possibleToolNames returns the names a request's tool has with and without
// connection qualification; false when this build cannot name it.
func (toolSet *ToolSet) possibleToolNames(request callstate.Request) ([]string, bool) {
	connector, isLinked := toolSet.registry.Connector(request.ConnectorID)
	if !isLinked {
		return nil, false
	}
	metadata, isExposed := connector.ExposedOperation(request.OperationID)
	if !isExposed {
		return nil, false
	}
	operationName := registry.OperationName(metadata)
	return []string{
		toolName(request.ConnectorID, request.ConnectionName, operationName, false),
		toolName(request.ConnectorID, request.ConnectionName, operationName, true),
	}, true
}

// projectSnapshot is one load of the project configuration.
type projectSnapshot struct {
	// project is nil when no connections file exists yet or it failed to load.
	project  *projectconfig.LoadedProject
	identity string
	warnings []string
}

// toolSetBuilder turns a project snapshot and a selection into a ToolSet.
type toolSetBuilder struct {
	registry  *registry.Registry
	newServer func(*ToolSet) *mcp.Server
	logger    *slog.Logger
}

// enabledConnection is one connection the selection, or its absence, enables.
type enabledConnection struct {
	connector      registry.LinkedConnector
	connectionName string
	tools          []string
}

func (builder *toolSetBuilder) build(snapshot projectSnapshot, selection *ConnectionSelection) *ToolSet {
	toolSet := &ToolSet{registry: builder.registry, projectIdentity: snapshot.identity, warnings: slices.Clone(snapshot.warnings)}
	toolSet.warnings = append(toolSet.warnings, selection.Warnings()...)
	enabled, namingCounts := builder.enableConnections(snapshot, selection, toolSet)
	var bindings []*toolBinding
	for _, connection := range enabled {
		bindings = append(bindings, builder.bindConnection(snapshot.project, connection, namingCounts[connection.connector.ConnectorID] > 1, toolSet)...)
	}
	toolSet.byName = map[string]*toolBinding{}
	toolSet.byIdentity = map[toolIdentity]*toolBinding{}
	collisions := map[string]int{}
	for _, binding := range bindings {
		collisions[binding.name]++
	}
	for _, binding := range bindings {
		if collisions[binding.name] > 1 {
			continue
		}
		toolSet.tools = append(toolSet.tools, binding)
		toolSet.byName[binding.name] = binding
		toolSet.byIdentity[binding.identity()] = binding
	}
	for name, count := range collisions {
		if count > 1 {
			toolSet.warnings = append(toolSet.warnings, fmt.Sprintf("%d tools would be named %s; none of them is offered", count, name))
		}
	}
	sort.Slice(toolSet.tools, func(left, right int) bool { return toolSet.tools[left].name < toolSet.tools[right].name })
	slices.Sort(toolSet.warnings)
	toolSet.digest = toolSetDigest(toolSet.tools)
	toolSet.mcpServer = builder.newServer(toolSet)
	for _, warning := range toolSet.warnings {
		builder.logger.Warn(warning)
	}
	return toolSet
}

// enableConnections returns the enabled connections and, per connector, the
// count that decides whether its tool names include the connection.
func (builder *toolSetBuilder) enableConnections(snapshot projectSnapshot, selection *ConnectionSelection, toolSet *ToolSet) ([]enabledConnection, map[string]int) {
	authorized := map[string][]string{}
	if snapshot.project != nil {
		for _, connection := range snapshot.project.Configuration.Connections {
			authorized[connection.ConnectorID] = append(authorized[connection.ConnectorID], connection.ConnectionName)
		}
	}
	namingCounts := map[string]int{}
	var enabled []enabledConnection
	if selection == nil {
		for _, connector := range builder.registry.Connectors() {
			names := slices.Clone(authorized[connector.ConnectorID])
			slices.Sort(names)
			for _, name := range slices.Compact(names) {
				enabled = append(enabled, enabledConnection{connector: connector, connectionName: name})
				namingCounts[connector.ConnectorID]++
			}
		}
		return enabled, namingCounts
	}
	for _, entry := range selection.Connections() {
		label := entry.ConnectorID + "/" + entry.ConnectionName
		connector, isLinked := builder.registry.Connector(entry.ConnectorID)
		if !isLinked {
			toolSet.warnings = append(toolSet.warnings, fmt.Sprintf("connection %s is skipped: connector %s is not included in this server", label, entry.ConnectorID))
			continue
		}
		namingCounts[entry.ConnectorID]++
		if !slices.Contains(authorized[entry.ConnectorID], entry.ConnectionName) {
			toolSet.warnings = append(toolSet.warnings, fmt.Sprintf("connection %s is skipped: it is not authorized in the connections file", label))
			continue
		}
		enabled = append(enabled, enabledConnection{connector: connector, connectionName: entry.ConnectionName, tools: entry.Tools})
	}
	return enabled, namingCounts
}

func (builder *toolSetBuilder) bindConnection(project *projectconfig.LoadedProject, connection enabledConnection, isQualified bool, toolSet *ToolSet) []*toolBinding {
	label := connection.connector.ConnectorID + "/" + connection.connectionName
	allowedOperations, unknownTools := resolveToolList(connection.connector, connection.tools)
	for _, unknown := range unknownTools {
		toolSet.warnings = append(toolSet.warnings, fmt.Sprintf("connection %s: tool %s is skipped: no exposed operation of %s has that name", label, unknown, connection.connector.ConnectorID))
	}
	connectionTarget, hasTarget := connectionTargetOf(project, connection.connector.ConnectorID, connection.connectionName)
	if !hasTarget {
		toolSet.warnings = append(toolSet.warnings, fmt.Sprintf("connection %s is skipped: it has no configuration", label))
		return nil
	}
	tools, err := connection.connector.OpenAgentTools(project, connection.connectionName)
	if err != nil {
		builder.logger.Warn("connection cannot be opened", "connection", label, "error", err)
		toolSet.warnings = append(toolSet.warnings, fmt.Sprintf("connection %s is skipped: it cannot be opened (see the server log)", label))
		return nil
	}
	var bindings []*toolBinding
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		metadata := tool.Metadata()
		exposed, isExposed := connection.connector.ExposedOperation(metadata.Operation.OperationID)
		if !isExposed || exposed.ToolName != metadata.ToolName || metadata.Operation.ConnectorID != connection.connector.ConnectorID {
			continue
		}
		if allowedOperations != nil && !allowedOperations[metadata.Operation.OperationID] {
			continue
		}
		if tool.Connection().Name != connection.connectionName {
			toolSet.warnings = append(toolSet.warnings, fmt.Sprintf("connection %s: operation %s is bound to another connection and is skipped", label, metadata.Operation.OperationID))
			continue
		}
		binding, err := newToolBinding(tool, metadata, connection, isQualified, connectionTarget)
		if err != nil {
			toolSet.warnings = append(toolSet.warnings, fmt.Sprintf("connection %s: operation %s is skipped: %v", label, metadata.Operation.OperationID, err))
			continue
		}
		bindings = append(bindings, binding)
	}
	return bindings
}

// resolveToolList maps tool list elements, snake-case operation names or
// operationIds, to exposed operation IDs; nil allows every exposed operation.
func resolveToolList(connector registry.LinkedConnector, tools []string) (map[string]bool, []string) {
	if tools == nil {
		return nil, nil
	}
	allowed := map[string]bool{}
	var unknown []string
	for _, element := range tools {
		isFound := false
		for _, metadata := range connector.ExposedAgentTools {
			if element == metadata.Operation.OperationID || element == registry.OperationName(metadata) {
				allowed[metadata.Operation.OperationID] = true
				isFound = true
			}
		}
		if !isFound {
			unknown = append(unknown, element)
		}
	}
	return allowed, unknown
}

func newToolBinding(tool sdkgo.AgentTool, metadata sdkgo.AgentToolMetadata, connection enabledConnection, isQualified bool, connectionTarget string) (*toolBinding, error) {
	var schemaObject map[string]any
	if err := json.Unmarshal(metadata.InputSchema, &schemaObject); err != nil || schemaObject["type"] != "object" {
		return nil, fmt.Errorf("its input schema is not a JSON Schema of type object")
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(metadata.InputSchema, &schema); err != nil {
		return nil, fmt.Errorf("its input schema does not decode: %w", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("its input schema does not resolve: %w", err)
	}
	operationName := registry.OperationName(metadata)
	effect := metadata.Effect()
	name := toolName(connection.connector.ConnectorID, connection.connectionName, operationName, isQualified)
	meta := mcp.Meta{
		metaKeyEffect:           string(effect),
		metaKeyConnector:        connection.connector.ConnectorID,
		metaKeyConnection:       connection.connectionName,
		metaKeyOperation:        metadata.Operation.OperationID,
		metaKeyOperationName:    operationName,
		metaKeyConnectorVersion: connection.connector.ModuleVersion,
	}
	// Dex requires approval whenever the key is present, so it is set only when recommended.
	if metadata.IsApprovalRecommended {
		meta[metaKeyApproval] = approvalRecommended
	}
	return &toolBinding{
		name:             name,
		tool:             tool,
		metadata:         metadata,
		connector:        connection.connector,
		connectionName:   connection.connectionName,
		effect:           effect,
		inputSchema:      resolved,
		connectionTarget: connectionTarget,
		mcpTool: &mcp.Tool{
			Meta:        meta,
			Name:        name,
			Title:       connection.connector.DisplayName + ": " + firstSentence(metadata.Description),
			Description: metadata.Description,
			InputSchema: json.RawMessage(metadata.InputSchema),
			Annotations: toolAnnotations(effect),
		},
	}, nil
}

func (binding *toolBinding) executionIdentity() executionIdentity {
	return executionIdentity{connectorID: binding.connector.ConnectorID, connectionName: binding.connectionName, operationID: binding.metadata.Operation.OperationID}
}

func (binding *toolBinding) identity() toolIdentity {
	return toolIdentity{connectorID: binding.connector.ConnectorID, operationID: binding.metadata.Operation.OperationID, connectionName: binding.connectionName}
}

// attemptTimeout is the operation's Execute timeout, or 60 seconds.
func (binding *toolBinding) attemptTimeout() time.Duration {
	if timeout := binding.metadata.StepDefaults.ExecuteMethodTimeout; timeout > 0 {
		return timeout
	}
	return defaultAttemptTimeout
}

// operationLabel is "<connectorId> <operationId>", the subject of result text.
func (binding *toolBinding) operationLabel() string {
	return binding.connector.ConnectorID + " " + binding.metadata.Operation.OperationID
}

// validateArguments returns a schema problem the model can act on, or "".
func (binding *toolBinding) validateArguments(arguments json.RawMessage) string {
	var instance any
	if err := json.Unmarshal(arguments, &instance); err != nil {
		return "arguments are not valid JSON"
	}
	if _, isObject := instance.(map[string]any); !isObject {
		return "arguments must be a JSON object"
	}
	if err := binding.inputSchema.Validate(instance); err != nil {
		return err.Error()
	}
	return ""
}

// toolAnnotations returns hints that agree with the effect, for clients that ignore _meta.
func toolAnnotations(effect sdkgo.AgentToolEffect) *mcp.ToolAnnotations {
	isOpenWorld := true
	switch effect {
	case sdkgo.AgentToolEffectRead:
		return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &isOpenWorld}
	case sdkgo.AgentToolEffectIdempotentWrite:
		return &mcp.ToolAnnotations{IdempotentHint: true, OpenWorldHint: &isOpenWorld}
	default:
		isDestructive := true
		return &mcp.ToolAnnotations{DestructiveHint: &isDestructive, OpenWorldHint: &isOpenWorld}
	}
}

func firstSentence(description string) string {
	trimmed := strings.TrimSpace(description)
	if index := strings.Index(trimmed, ". "); index >= 0 {
		return trimmed[:index]
	}
	return strings.TrimSuffix(trimmed, ".")
}

func toolSetDigest(tools []*toolBinding) string {
	described := make([]any, 0, len(tools))
	for _, binding := range tools {
		described = append(described, map[string]any{"name": binding.name, "meta": binding.mcpTool.Meta, "description": binding.metadata.Description, "inputSchema": binding.metadata.InputSchema})
	}
	canonical, err := canonicaljson.Marshal(described)
	if err != nil {
		panic(fmt.Sprintf("encode tool set: %v", err))
	}
	return "sha256:" + canonicaljson.SHA256Hex(canonical)
}
