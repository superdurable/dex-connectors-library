// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// RowAddedTriggerRoute binds one rowAdded binding's configuration and cursor to its application target.
type RowAddedTriggerRoute struct {
	// BindingName names the configured Trigger binding; it is unique within one runner.
	BindingName string
	// Configuration selects the polled spreadsheet tab.
	Configuration RowAddedTriggerConfiguration
	// Cursor persists the binding's last processed row, normally LoadedProject.PollCursorStore. Use
	// pollingtrigger.NewMemoryCursorStore only when rows added while the process is stopped may be skipped.
	Cursor pollingtrigger.CursorStore
	// Target receives each added row. A target wrapped by provider.NewDurableTriggerTarget records the row
	// before the cursor moves past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[RowAddedEvent]
}

// RowAddedTriggerRunnerConfig configures every rowAdded binding of one Google Sheets connection.
type RowAddedTriggerRunnerConfig struct {
	// Connection is the Google Sheets connection whose pollInterval and pollPageSize settings apply.
	Connection Connection
	// Routes are polled in this order whenever several are due; at least one is required.
	Routes []RowAddedTriggerRoute
	// RequestLimiter optionally paces every poll request, for example to share a per-user Sheets quota.
	RequestLimiter pollingtrigger.RequestLimiter
}

// RowAddedTriggerRunner polls each rowAdded binding's tab from one goroutine.
type RowAddedTriggerRunner struct {
	runner *pollingtrigger.Runner
}

// NewRowAddedTriggerRunner validates every route and builds one poll runner for the connection. It makes
// no provider request and reads no storage.
//
// Each binding seeds its cursor at the tab's last row on its first poll, so existing rows never start
// Flows. Later polls read the header and up to pollPageSize rows after the last processed row with one
// values:batchGet request, and deliver each non-empty row, in row order, before the cursor commits. The
// cursor stores the last processed row number and a SHA-256 of its cells: when rows above it are deleted
// or inserted, the runner finds that row again and continues after it; when it was edited in place, the
// runner keeps its position; when the tab now ends above it, the runner reseeds and logs the skipped gap.
// Delivery is at least once, and each event ID is a pure function of the tab, row number, and cells.
func NewRowAddedTriggerRunner(config RowAddedTriggerRunnerConfig) (*RowAddedTriggerRunner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.Routes) == 0 {
		return nil, errors.New("Google Sheets rowAdded Trigger routes are required")
	}
	client := config.Connection.client
	pollers := make([]pollingtrigger.Poller, 0, len(config.Routes))
	for _, route := range config.Routes {
		if strings.TrimSpace(route.BindingName) == "" || route.Target == nil || route.Cursor == nil {
			return nil, errors.New("Google Sheets rowAdded binding name, cursor, and target are required")
		}
		if err := route.Configuration.Validate(); err != nil {
			return nil, fmt.Errorf("Google Sheets rowAdded binding %q: %w", route.BindingName, err)
		}
		binding := sdkgo.TriggerBindingRef{Connection: config.Connection.reference, Trigger: RowAddedTriggerDefinition.Trigger, Name: route.BindingName}
		poller, err := pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[RowAddedEvent]{
			Definition: RowAddedTriggerDefinition, Binding: binding, Cursor: route.Cursor,
			Fetcher: client.newRowAddedFetcher(binding, route.Configuration), Target: route.Target,
		})
		if err != nil {
			return nil, fmt.Errorf("Google Sheets rowAdded binding %q: %w", route.BindingName, err)
		}
		pollers = append(pollers, poller)
	}
	runner, err := pollingtrigger.NewRunner(pollingtrigger.RunnerConfig{
		Connection: config.Connection.reference, Schedule: client.rowAddedPollSchedule(), Pollers: pollers,
		RequestLimiter: config.RequestLimiter, Logger: client.logger,
	})
	if err != nil {
		return nil, err
	}
	return &RowAddedTriggerRunner{runner: runner}, nil
}

// Run replays each durable target's pending rows, seeds missing cursors, and then polls every binding until
// ctx ends. A failed poll backs off exponentially up to five minutes, and HTTP 429 pauses every binding of
// the connection for at least its Retry-After. Run returns ctx.Err() on cancellation, or an error for a
// stored cursor document that can never be decoded. Records go to the connection's WithLogger logger.
func (runner *RowAddedTriggerRunner) Run(ctx context.Context) error {
	return runner.runner.Run(ctx)
}

// ProjectRowAddedTriggerRoute binds one rowAdded binding saved in the project configuration to its target.
type ProjectRowAddedTriggerRoute struct {
	// BindingName names the binding whose spreadsheet and tab Dex Web saved.
	BindingName string
	// Target receives each added row after the binding's durable project inbox records it.
	Target sdkgo.TriggerTarget[RowAddedEvent]
}

// NewProjectRowAddedTriggerRunner opens connectionName from the loaded project, loads each route's saved
// binding configuration, and builds a durable runner: each binding's cursor lives in project storage beside
// its Trigger inbox, and every row is recorded in that inbox before the cursor commits past it. A restarted
// or replaced replica therefore neither misses nor replays a row. WithLogger also applies to the inboxes.
func NewProjectRowAddedTriggerRunner(
	project *projectconfig.LoadedProject,
	connectionName string,
	routes []ProjectRowAddedTriggerRoute,
	options ...Option,
) (*RowAddedTriggerRunner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableRowAddedTriggerRunner(connection, project.Configuration, project.TriggerInbox,
		func(key projectconfig.PollCursorKey) (pollingtrigger.CursorStore, error) {
			return project.PollCursorStore(key)
		}, routes)
}

// newDurableRowAddedTriggerRunner opens each route's inbox and cursor with the supplied project storage.
func newDurableRowAddedTriggerRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	openInbox func(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error),
	openCursor func(projectconfig.PollCursorKey) (pollingtrigger.CursorStore, error),
	routes []ProjectRowAddedTriggerRoute,
) (*RowAddedTriggerRunner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := RowAddedTriggerDefinition.Trigger.TriggerName
	runnerConfig := RowAddedTriggerRunnerConfig{Connection: connection}
	for _, route := range routes {
		if route.Target == nil {
			return nil, fmt.Errorf("Google Sheets rowAdded binding %q target is required", route.BindingName)
		}
		var bindingConfiguration RowAddedTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration); err != nil {
			return nil, fmt.Errorf("Google Sheets rowAdded binding %q configuration: %w", route.BindingName, err)
		}
		key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: connection.reference.Name, TriggerName: triggerName, BindingName: route.BindingName}
		inbox, err := openInbox(key)
		if err != nil {
			return nil, err
		}
		target, err := provider.NewDurableTriggerTarget(inbox, key, route.Target, provider.WithTriggerLogger(connection.client.logger))
		if err != nil {
			return nil, err
		}
		cursor, err := openCursor(projectconfig.PollCursorKey(key))
		if err != nil {
			return nil, err
		}
		runnerConfig.Routes = append(runnerConfig.Routes, RowAddedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Cursor: cursor, Target: target,
		})
	}
	return NewRowAddedTriggerRunner(runnerConfig)
}
