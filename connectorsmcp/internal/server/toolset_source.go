// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/canonicaljson"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// useConfigurationsFileName is the sidecar Dex Web writes beside connections.json.
const useConfigurationsFileName = "use-configurations.json"

// projectSource loads the project configuration and fingerprints its files.
type projectSource interface {
	// fingerprint changes whenever a load could return something new.
	fingerprint() string
	load(ctx context.Context) projectSnapshot
}

// dexWebProjectSource loads the connections.json that Dex Web writes.
type dexWebProjectSource struct {
	path   string
	logger *slog.Logger
}

func (source dexWebProjectSource) fingerprint() string {
	return fileFingerprint(source.path) + "|" + fileFingerprint(filepath.Join(filepath.Dir(source.path), useConfigurationsFileName))
}

func (source dexWebProjectSource) load(ctx context.Context) projectSnapshot {
	identity := "file:" + source.path
	if resolved, err := filepath.EvalSymlinks(source.path); err == nil {
		identity = "file:" + resolved
	}
	if _, err := os.Lstat(source.path); errors.Is(err, fs.ErrNotExist) {
		return projectSnapshot{identity: identity, warnings: []string{"the connections file does not exist yet; authorize a connection in Dex Web"}}
	}
	project, err := projectconfig.LoadDexWebConnectionsFile(ctx, source.path)
	if err != nil {
		source.logger.Error("connections file cannot be loaded; serving no tools", "file", source.path, "error", err)
		return projectSnapshot{identity: identity, warnings: []string{"the connections file cannot be loaded; serving no tools (see the server log)"}}
	}
	return projectSnapshot{project: project, identity: identity, canVerifyConnectionTargets: true}
}

// hostedProjectSource serves one project loaded from DEX_PROJECT_* at start;
// changes need a restart.
type hostedProjectSource struct {
	snapshot projectSnapshot
}

func newHostedProjectSource(ctx context.Context) (hostedProjectSource, error) {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return hostedProjectSource{}, fmt.Errorf("load the hosted project configuration: %w", err)
	}
	scope := project.Configuration.Scope
	return hostedProjectSource{snapshot: projectSnapshot{project: project, identity: "project:" + scope.ProjectID + "/" + scope.Kind}}, nil
}

func (source hostedProjectSource) fingerprint() string { return "hosted" }

func (source hostedProjectSource) load(context.Context) projectSnapshot { return source.snapshot }

// selectionSource supplies the connection selection.
type selectionSource interface {
	fingerprint() string
	load() (*ConnectionSelection, error)
}

// fixedSelectionSource is a selection from flags, or no selection.
type fixedSelectionSource struct{ selection *ConnectionSelection }

func (fixedSelectionSource) fingerprint() string { return "" }

func (source fixedSelectionSource) load() (*ConnectionSelection, error) { return source.selection, nil }

// fileSelectionSource re-reads a --connections-config file when it changes.
type fileSelectionSource struct{ path string }

func (source fileSelectionSource) fingerprint() string { return fileFingerprint(source.path) }

func (source fileSelectionSource) load() (*ConnectionSelection, error) {
	return ReadConnectionsConfigFile(source.path)
}

// toolSetSource builds the ToolSet and rebuilds it when the connections file,
// its sidecar, or the selection file changes.
type toolSetSource struct {
	projects   projectSource
	selections selectionSource
	builder    *toolSetBuilder
	onChange   func(toolSet *ToolSet, connectionsConfigDigest *string)
	logger     *slog.Logger

	mu                 sync.Mutex
	current            *ToolSet
	currentFingerprint string
	selection          *ConnectionSelection
}

// newToolSetSource loads the selection, which must be valid at start, and the
// first ToolSet.
func newToolSetSource(ctx context.Context, projects projectSource, selections selectionSource, builder *toolSetBuilder, onChange func(*ToolSet, *string), logger *slog.Logger) (*toolSetSource, error) {
	selection, err := selections.load()
	if err != nil {
		return nil, err
	}
	source := &toolSetSource{projects: projects, selections: selections, builder: builder, onChange: onChange, logger: logger, selection: selection}
	source.mu.Lock()
	defer source.mu.Unlock()
	source.rebuildLocked(ctx, source.fingerprint())
	return source, nil
}

// Current returns the ToolSet for the files as they are now.
func (source *toolSetSource) Current(ctx context.Context) *ToolSet {
	fingerprint := source.fingerprint()
	source.mu.Lock()
	defer source.mu.Unlock()
	if fingerprint != source.currentFingerprint {
		source.rebuildLocked(ctx, fingerprint)
	}
	return source.current
}

// watch checks for changes every interval until ctx ends.
func (source *toolSetSource) watch(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			source.Current(ctx)
		}
	}
}

func (source *toolSetSource) fingerprint() string {
	return source.projects.fingerprint() + "#" + source.selections.fingerprint()
}

// rebuildLocked rebuilds the set; onChange runs for a changed set after the first.
func (source *toolSetSource) rebuildLocked(ctx context.Context, fingerprint string) {
	previous := source.current
	previousSelectionDigest := source.selection.ConnectionsConfigDigest()
	if previous != nil {
		selection, err := source.selections.load()
		if err != nil {
			source.logger.Warn("connections config cannot be reloaded; keeping the previous selection", "error", err)
		} else {
			source.selection = selection
		}
	}
	source.current = source.builder.build(source.projects.load(ctx), source.selection)
	source.currentFingerprint = fingerprint
	selectionDigest := source.selection.ConnectionsConfigDigest()
	isSelectionChanged := (previousSelectionDigest == nil) != (selectionDigest == nil) ||
		(selectionDigest != nil && *selectionDigest != *previousSelectionDigest)
	if previous != nil && source.onChange != nil && (previous.digest != source.current.digest || isSelectionChanged || !slices.Equal(previous.warnings, source.current.warnings)) {
		source.onChange(source.current, selectionDigest)
	}
}

// selectionDigest returns the connectionsConfigDigest of the selection in use.
func (source *toolSetSource) selectionDigest() *string {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.selection.ConnectionsConfigDigest()
}

// fileFingerprint changes whenever the file is created, removed, or rewritten.
func fileFingerprint(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return "missing"
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("unreadable:%d:%d", info.Size(), info.ModTime().UnixNano())
	}
	return fmt.Sprintf("%d:%d:%o:%s", info.Size(), info.ModTime().UnixNano(), info.Mode(), canonicaljson.SHA256Hex(contents))
}
