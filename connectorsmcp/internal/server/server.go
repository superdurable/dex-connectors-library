// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package server serves exposed connector operations as MCP tools under the Dex conventions.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/callstate"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
)

const (
	// MCPPath is the HTTP path of the MCP endpoint.
	MCPPath = "/mcp"
	// HealthPath is the HTTP path of the health endpoint.
	HealthPath = "/healthz"
	// maximumInlineAttempts caps attempts per read call and per write key.
	maximumInlineAttempts = 3
	// reloadInterval is how often the files are checked besides each request.
	reloadInterval = 5 * time.Second
	// pruneInterval is how often expired call state is removed.
	pruneInterval = time.Hour
	// minimumCallStateRetention is the shortest retention accepted.
	minimumCallStateRetention = 24 * time.Hour
)

// ToolSetConfig says where tools come from.
type ToolSetConfig struct {
	// Registry lists the linked connectors.
	Registry *registry.Registry
	// ConnectionsFile is the absolute path of the Dex Web connections.json;
	// empty loads the hosted project from DEX_PROJECT_* once.
	ConnectionsFile string
	// Selection is the selection from --connection flags; nil is none.
	Selection *ConnectionSelection
	// ConnectionsConfigFile, when set, supplies the selection instead and is
	// re-read when it changes.
	ConnectionsConfigFile string
	// Logger receives warnings; it never receives arguments or results.
	Logger *slog.Logger
}

// Config configures a Server.
type Config struct {
	// Version is the server version reported to clients.
	Version string
	// InstanceID identifies this process in /healthz and server.json.
	InstanceID string
	// ToolSet says where tools come from.
	ToolSet ToolSetConfig
	// Store is the opened call-state store.
	Store *callstate.Store
	// AllowedHosts are extra Host names accepted besides loopback names.
	AllowedHosts []string
	// CallTimeBudget bounds the inline retries of one call.
	CallTimeBudget time.Duration
	// CallStateRetention is how long write records are kept.
	CallStateRetention time.Duration
	// MaxResultBytes is the longest result text before truncation.
	MaxResultBytes int
	// Clock drives retry delays, budgets, and retry windows.
	Clock Clock
	// OnToolSetChange runs after a reload changed the tools, the warnings, or
	// the selection; nil does nothing.
	OnToolSetChange func(toolSet *ToolSet, connectionsConfigDigest *string)
	// ObserveProtocolPoint is for crash-injection tests; nil in production.
	ObserveProtocolPoint func(point ProtocolPoint, operationID string)
}

// Server is the MCP endpoint and its health endpoint.
type Server struct {
	config   *Config
	executor *toolCallExecutor
	source   *toolSetSource
	handler  http.Handler
}

// New validates limits and the retention, then loads the first tool set.
func New(ctx context.Context, config *Config) (*Server, error) {
	if config == nil || config.ToolSet.Registry == nil || config.ToolSet.Logger == nil || config.Store == nil || config.Clock == nil {
		panic("server.New requires a config with a registry, a logger, a store, and a clock")
	}
	if err := validateLimits(config); err != nil {
		return nil, err
	}
	executor := newToolCallExecutor(config.Store, config.Clock, executorConfig{
		serverVersion: config.Version, callTimeBudget: config.CallTimeBudget, callStateRetention: config.CallStateRetention,
		maxResultBytes: config.MaxResultBytes, maximumInlineAttempts: maximumInlineAttempts,
	}, config.ToolSet.Logger, config.ObserveProtocolPoint)
	builder := &toolSetBuilder{registry: config.ToolSet.Registry, logger: config.ToolSet.Logger, newServer: func(toolSet *ToolSet) *mcp.Server {
		return newMCPServer(config.Version, toolSet, executor)
	}}
	source, err := newToolSetSourceFromConfig(ctx, &config.ToolSet, builder, config.OnToolSetChange)
	if err != nil {
		return nil, err
	}
	server := &Server{config: config, executor: executor, source: source}
	streamable := mcp.NewStreamableHTTPHandler(
		func(request *http.Request) *mcp.Server { return source.Current(request.Context()).mcpServer },
		// The Host policy below replaces the SDK's localhost check, which would refuse allowed hosts.
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true, Logger: config.ToolSet.Logger},
	)
	mux := http.NewServeMux()
	mux.Handle(MCPPath, streamable)
	mux.HandleFunc(HealthPath, server.serveHealth)
	server.handler = newRequestHostPolicy(config.AllowedHosts).protect(mux)
	return server, nil
}

// LoadToolSet builds the tool set once without serving, for list-tools.
func LoadToolSet(ctx context.Context, config *ToolSetConfig) (*ToolSet, error) {
	builder := &toolSetBuilder{registry: config.Registry, logger: config.Logger, newServer: func(*ToolSet) *mcp.Server { return nil }}
	source, err := newToolSetSourceFromConfig(ctx, config, builder, nil)
	if err != nil {
		return nil, err
	}
	return source.Current(ctx), nil
}

func newToolSetSourceFromConfig(ctx context.Context, config *ToolSetConfig, builder *toolSetBuilder, onChange func(*ToolSet, *string)) (*toolSetSource, error) {
	var projects projectSource = dexWebProjectSource{path: config.ConnectionsFile, logger: config.Logger}
	if config.ConnectionsFile == "" {
		hosted, err := newHostedProjectSource(ctx)
		if err != nil {
			return nil, err
		}
		projects = hosted
	}
	var selections selectionSource = fixedSelectionSource{selection: config.Selection}
	if config.ConnectionsConfigFile != "" {
		if config.Selection != nil {
			return nil, errors.New("--connection and --connections-config are mutually exclusive")
		}
		selections = fileSelectionSource{path: config.ConnectionsConfigFile}
	}
	return newToolSetSource(ctx, projects, selections, builder, onChange, config.Logger)
}

func validateLimits(config *Config) error {
	switch {
	case config.CallTimeBudget <= 0:
		return errors.New("--call-time-budget must be positive")
	case config.MaxResultBytes < 1024:
		return errors.New("--max-result-bytes must be at least 1024")
	case config.CallStateRetention < minimumCallStateRetention:
		return fmt.Errorf("--call-state-retention must be at least %s", minimumCallStateRetention)
	}
	for _, connector := range config.ToolSet.Registry.Connectors() {
		for _, metadata := range connector.ExposedAgentTools {
			window := retryPolicyOf(metadata).TotalDuration
			if window <= 0 {
				window = time.Hour
			}
			if window > config.CallStateRetention {
				return fmt.Errorf("--call-state-retention %s is shorter than the retry window %s of %s %s", config.CallStateRetention, window, connector.ConnectorID, metadata.Operation.OperationID)
			}
		}
	}
	return nil
}

// Handler serves MCPPath and HealthPath behind the Host and Origin checks.
func (server *Server) Handler() http.Handler { return server.handler }

// CurrentToolSet returns the tool set for the files as they are now.
func (server *Server) CurrentToolSet(ctx context.Context) *ToolSet { return server.source.Current(ctx) }

// ConnectionsConfigDigest returns the digest of the selection in use, or nil.
func (server *Server) ConnectionsConfigDigest() *string { return server.source.selectionDigest() }

// RunMaintenance reloads changed files every 5 seconds and prunes expired
// call state at start and hourly, until ctx ends.
func (server *Server) RunMaintenance(ctx context.Context) {
	go server.source.watch(ctx, reloadInterval)
	server.prune(ctx)
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			server.prune(ctx)
		}
	}
}

func (server *Server) prune(ctx context.Context) {
	result, err := server.config.Store.Prune(ctx, server.config.CallStateRetention)
	if err != nil {
		server.config.ToolSet.Logger.Warn("expired call state was not all removed", "error", err)
	}
	if result != (callstate.PruneResult{}) {
		server.config.ToolSet.Logger.Info("pruned call state", "records", result.RemovedRecords, "lockFiles", result.RemovedLockFiles, "temporaryFiles", result.RemovedTemporaryFiles)
	}
}

// BeginDrain makes new calls answer that the server is stopping: a
// non-idempotent write with no state is rejected, never interrupted.
func (server *Server) BeginDrain() { server.executor.beginDrain() }

// WaitForInflightCalls waits for running calls until ctx ends.
func (server *Server) WaitForInflightCalls(ctx context.Context) error {
	return server.executor.waitForInflightCalls(ctx)
}

// MaximumAttemptTimeout is the longest attempt timeout of any linked operation.
func (server *Server) MaximumAttemptTimeout() time.Duration {
	longest := defaultAttemptTimeout
	for _, connector := range server.config.ToolSet.Registry.Connectors() {
		for _, metadata := range connector.ExposedAgentTools {
			longest = max(longest, metadata.StepDefaults.ExecuteMethodTimeout)
		}
	}
	return longest
}

// HealthStatus is the body of GET /healthz.
type HealthStatus struct {
	// Status is "ok".
	Status string `json:"status"`
	// Version is the server version.
	Version string `json:"version"`
	// LaunchContract is LaunchContract.
	LaunchContract int `json:"launchContract"`
	// InstanceID identifies this process.
	InstanceID string `json:"instanceId"`
	// ToolCount is the number of offered tools.
	ToolCount int `json:"toolCount"`
}

func (server *Server) serveHealth(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(response, "use GET", http.StatusMethodNotAllowed)
		return
	}
	toolSet := server.source.Current(request.Context())
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(HealthStatus{
		Status: "ok", Version: server.config.Version, LaunchContract: LaunchContract,
		InstanceID: server.config.InstanceID, ToolCount: len(toolSet.tools),
	}); err != nil {
		server.config.ToolSet.Logger.Warn("health response was not written", "error", err)
	}
}

// newMCPServer routes tools/call through the executor, which reads key state first.
func newMCPServer(version string, toolSet *ToolSet, executor *toolCallExecutor) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: ImplementationName, Version: version}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})
	for _, binding := range toolSet.tools {
		server.AddTool(binding.mcpTool, refuseDirectToolCall)
	}
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			callRequest, isCallTool := request.(*mcp.CallToolRequest)
			if method != "tools/call" || !isCallTool || callRequest.Params == nil {
				return next(ctx, method, request)
			}
			result, err := executor.callTool(ctx, toolSet, callRequest.Params)
			if err != nil {
				return nil, err
			}
			return result, nil
		}
	})
	return server
}

func refuseDirectToolCall(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "tools/call bypassed the call-state middleware"}
}
