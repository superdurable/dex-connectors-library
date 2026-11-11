// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/callstate"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/server"
)

const (
	serverLockFileName = "server.lock"
	serverFileName     = "server.json"
	// shutdownGrace is added to the longest attempt timeout while draining.
	shutdownGrace = 5 * time.Second
)

// serveFlags are the flags of serve.
type serveFlags struct {
	selection          connectionSelectionFlags
	listen             string
	stateDirectory     string
	allowedHosts       repeatedFlag
	callTimeBudget     time.Duration
	callStateRetention time.Duration
	maxResultBytes     int
	logFormat          string
	outputFormat       string
	isExitOnStdinClose bool
}

// ReadyAnnouncement is the ready line printed by serve --output json and the
// content of server.json.
type ReadyAnnouncement struct {
	// Type is "ready".
	Type string `json:"type"`
	// URL is the MCP endpoint.
	URL string `json:"url"`
	// HealthURL is the health endpoint.
	HealthURL string `json:"healthUrl"`
	// Version is the server version.
	Version string `json:"version"`
	// LaunchContract is server.LaunchContract.
	LaunchContract int `json:"launchContract"`
	// InstanceID identifies this process; /healthz reports the same value.
	InstanceID string `json:"instanceId"`
	// PID is the process ID.
	PID int `json:"pid"`
	// StateDir is the absolute state directory.
	StateDir string `json:"stateDir"`
	// ConnectionsFile is the absolute, cleaned connections.json path, or null
	// for a hosted project.
	ConnectionsFile *string `json:"connectionsFile"`
	// ConnectionsConfigDigest identifies the selection, or null for none.
	ConnectionsConfigDigest *string `json:"connectionsConfigDigest"`
	// ToolSetDigest changes when the offered tools change.
	ToolSetDigest string `json:"toolSetDigest"`
	// Tools are the offered tool names, sorted.
	Tools []string `json:"tools"`
	// Warnings are skipped selection entries and other non-fatal problems.
	Warnings []string `json:"warnings"`
}

func runServe(args []string, environment *Environment) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(environment.Stderr)
	var options serveFlags
	options.selection.register(flags)
	flags.StringVar(&options.listen, "listen", "127.0.0.1:8803", "loopback HOST:PORT; port 0 picks a free port")
	flags.StringVar(&options.stateDirectory, "state-dir", "", "state directory (default $HOME/.dex/connectors-mcp)")
	flags.Var(&options.allowedHosts, "allowed-host", "extra Host name accepted, such as a local reverse proxy's (repeatable)")
	flags.DurationVar(&options.callTimeBudget, "call-time-budget", 5*time.Minute, "time one call may spend on inline retries")
	flags.DurationVar(&options.callStateRetention, "call-state-retention", 720*time.Hour, "how long write call state is kept; at least 24h")
	flags.IntVar(&options.maxResultBytes, "max-result-bytes", 65536, "longest result text before truncation")
	flags.StringVar(&options.logFormat, "log-format", "text", "text or json (stderr)")
	flags.StringVar(&options.outputFormat, "output", "text", "ready announcement format on stdout: text or json")
	flags.BoolVar(&options.isExitOnStdinClose, "exit-on-stdin-close", false, "exit gracefully when stdin reaches end of file")
	if err := flags.Parse(args); err != nil {
		return exitCodeUsage
	}
	if flags.NArg() > 0 {
		return reportError(environment, options.outputFormat, usageError{fmt.Errorf("unexpected argument %q", flags.Arg(0))}, exitCodeUsage)
	}
	if err := validateOutputFormat(options.outputFormat); err != nil {
		return reportError(environment, "text", err, exitCodeUsage)
	}
	process, err := startServing(&options, environment)
	if err != nil {
		var inUse stateDirectoryInUseError
		if errors.As(err, &inUse) {
			return reportError(environment, options.outputFormat, err, exitCodeStateDirInUse)
		}
		return reportError(environment, options.outputFormat, err, exitCodeOf(err))
	}
	return process.serveUntilStopped(environment, options.isExitOnStdinClose)
}

// servingProcess is a started server with its lock and discovery file.
type servingProcess struct {
	logger       *slog.Logger
	server       *server.Server
	httpServer   *http.Server
	listener     net.Listener
	instanceLock *os.File
	serverFile   string
	served       chan error

	announcementMu sync.Mutex
	announcement   ReadyAnnouncement
	isAnnounced    bool
}

// stateDirectoryInUseError reports another serve holding the state directory.
type stateDirectoryInUseError struct{ message string }

func (err stateDirectoryInUseError) Error() string { return err.message }

func startServing(options *serveFlags, environment *Environment) (*servingProcess, error) {
	if err := server.ValidateListenAddress(options.listen); err != nil {
		return nil, usageError{err}
	}
	logger, err := newLogger(environment.Stderr, options.logFormat)
	if err != nil {
		return nil, err
	}
	toolSetConfig, err := options.selection.resolve(environment, logger)
	if err != nil {
		return nil, err
	}
	stateDirectory, err := resolveStateDirectory(options.stateDirectory, environment)
	if err != nil {
		return nil, err
	}
	if err := callstate.PrepareStateDirectory(stateDirectory); err != nil {
		return nil, err
	}
	process := &servingProcess{logger: logger, serverFile: filepath.Join(stateDirectory, serverFileName), served: make(chan error, 1)}
	if process.instanceLock, err = lockInstance(stateDirectory, process.serverFile); err != nil {
		return nil, err
	}
	store, err := callstate.Open(stateDirectory, time.Now)
	if err != nil {
		return nil, errors.Join(err, process.releaseInstanceLock())
	}
	instanceID := uuid.NewString()
	process.server, err = server.New(contextForCommand(), &server.Config{
		Version: environment.Version, InstanceID: instanceID, ToolSet: *toolSetConfig, Store: store,
		AllowedHosts: options.allowedHosts, CallTimeBudget: options.callTimeBudget, CallStateRetention: options.callStateRetention,
		MaxResultBytes: options.maxResultBytes, Clock: server.SystemClock{}, OnToolSetChange: process.announceToolSetChange,
		ObserveProtocolPoint: environment.ObserveProtocolPoint,
	})
	if err != nil {
		return nil, errors.Join(usageError{err}, process.releaseInstanceLock())
	}
	if process.listener, err = net.Listen("tcp", options.listen); err != nil {
		return nil, errors.Join(fmt.Errorf("listen on %s: %w", options.listen, err), process.releaseInstanceLock())
	}
	process.httpServer = &http.Server{Handler: process.server.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	go func() { process.served <- process.httpServer.Serve(process.listener) }()

	baseURL := "http://" + process.listener.Addr().String()
	toolSet := process.server.CurrentToolSet(contextForCommand())
	var connectionsFile *string
	if toolSetConfig.ConnectionsFile != "" {
		connectionsFile = &toolSetConfig.ConnectionsFile
	}
	process.announcement = ReadyAnnouncement{
		Type: "ready", URL: baseURL + server.MCPPath, HealthURL: baseURL + server.HealthPath, Version: environment.Version,
		LaunchContract: server.LaunchContract, InstanceID: instanceID, PID: os.Getpid(), StateDir: stateDirectory,
		ConnectionsFile: connectionsFile, ConnectionsConfigDigest: process.server.ConnectionsConfigDigest(),
		ToolSetDigest: toolSet.Digest(), Tools: nonNilStrings(toolSet.ToolNames()), Warnings: nonNilStrings(toolSet.Warnings()),
	}
	if err := process.announce(environment, options.outputFormat); err != nil {
		return nil, errors.Join(err, process.stop(context.Background()))
	}
	return process, nil
}

func resolveStateDirectory(flagValue string, environment *Environment) (string, error) {
	if flagValue != "" {
		return absoluteCleanPath(flagValue)
	}
	home, err := environment.HomeDirectory()
	if err != nil {
		return "", fmt.Errorf("find the home directory: %w", err)
	}
	return filepath.Join(home, ".dex", "connectors-mcp"), nil
}

// lockInstance holds server.lock for the process lifetime; a second serve on
// the state directory fails naming the running instance.
func lockInstance(stateDirectory string, serverFile string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(stateDirectory, serverLockFileName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", serverLockFileName, err)
	}
	isLocked, err := tryLockFile(file)
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !isLocked {
		message := "another dex-connectors-mcp serves state directory " + stateDirectory
		if contents, readErr := os.ReadFile(serverFile); readErr == nil {
			var running ReadyAnnouncement
			if json.Unmarshal(contents, &running) == nil && running.URL != "" {
				message += " at " + running.URL
			}
		}
		return nil, errors.Join(stateDirectoryInUseError{message: message}, file.Close())
	}
	return file, nil
}

// announce writes server.json, then prints the ready line, so a reader of the
// ready line always finds server.json.
func (process *servingProcess) announce(environment *Environment, outputFormat string) error {
	process.announcementMu.Lock()
	defer process.announcementMu.Unlock()
	if err := writeServerFile(process.serverFile, process.announcement); err != nil {
		return err
	}
	process.isAnnounced = true
	if outputFormat == "json" {
		encoded, err := json.Marshal(process.announcement)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(environment.Stdout, string(encoded))
		return err
	}
	_, err := fmt.Fprintf(environment.Stdout, "dex-connectors-mcp %s serving %d tools at %s\n", process.announcement.Version, len(process.announcement.Tools), process.announcement.URL)
	return err
}

// announceToolSetChange rewrites server.json after a reload changed the tools.
func (process *servingProcess) announceToolSetChange(toolSet *server.ToolSet, connectionsConfigDigest *string) {
	process.announcementMu.Lock()
	defer process.announcementMu.Unlock()
	process.announcement.ToolSetDigest = toolSet.Digest()
	process.announcement.Tools = nonNilStrings(toolSet.ToolNames())
	process.announcement.Warnings = nonNilStrings(toolSet.Warnings())
	process.announcement.ConnectionsConfigDigest = connectionsConfigDigest
	if !process.isAnnounced {
		return
	}
	if err := writeServerFile(process.serverFile, process.announcement); err != nil {
		process.logger.Warn("server.json was not rewritten", "error", err)
	}
}

// serveUntilStopped serves until a signal, stdin end of file, or a serve
// failure, then drains and exits.
func (process *servingProcess) serveUntilStopped(environment *Environment, isExitOnStdinClose bool) int {
	maintenanceContext, stopMaintenance := context.WithCancel(context.Background())
	defer stopMaintenance()
	go process.server.RunMaintenance(maintenanceContext)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	stdinClosed := make(chan struct{})
	if isExitOnStdinClose {
		go func() {
			// Any read error, like end of file, means the parent is gone.
			_, _ = io.Copy(io.Discard, environment.Stdin)
			close(stdinClosed)
		}()
	}
	exitCode := exitCodeOK
	select {
	case received := <-signals:
		process.logger.Info("stopping", "signal", received.String())
	case <-stdinClosed:
		process.logger.Info("stopping: stdin reached end of file")
	case err := <-process.served:
		process.logger.Error("serving failed", "error", err)
		exitCode = exitCodeFailure
	}
	stopContext, cancel := context.WithTimeout(context.Background(), process.server.MaximumAttemptTimeout()+shutdownGrace)
	defer cancel()
	if err := process.stop(stopContext); err != nil {
		process.logger.Warn("stopped with errors; a write still in flight resolves through its checkpoint on the next call", "error", err)
	}
	return exitCode
}

// stop drains calls, closes the listener, removes server.json, and releases
// the instance lock.
func (process *servingProcess) stop(ctx context.Context) error {
	process.server.BeginDrain()
	shutdownErr := process.httpServer.Shutdown(ctx)
	drainErr := process.server.WaitForInflightCalls(ctx)
	removeErr := os.Remove(process.serverFile)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(shutdownErr, drainErr, removeErr, process.releaseInstanceLock())
}

func (process *servingProcess) releaseInstanceLock() error {
	if process.instanceLock == nil {
		return nil
	}
	err := unlockAndCloseFile(process.instanceLock)
	process.instanceLock = nil
	return err
}

// writeServerFile replaces server.json atomically with mode 0600.
func writeServerFile(path string, announcement ReadyAnnouncement) error {
	encoded, err := json.MarshalIndent(announcement, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+serverFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("write server.json: %w", err)
	}
	_, writeErr := temporary.Write(append(encoded, '\n'))
	closeErr := temporary.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return errors.Join(fmt.Errorf("write server.json: %w", err), os.Remove(temporary.Name()))
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return errors.Join(fmt.Errorf("write server.json: %w", err), os.Remove(temporary.Name()))
	}
	return nil
}

func contextForCommand() context.Context { return context.Background() }
