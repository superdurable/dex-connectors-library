// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

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

// RecordChangedTriggerRoute binds one recordChanged binding's configuration and cursor to its application target.
type RecordChangedTriggerRoute struct {
	// BindingName names the configured Trigger binding; it is unique within one runner.
	BindingName string
	// Configuration selects the polled table and its Last modified time field.
	Configuration RecordChangedTriggerConfiguration
	// Cursor persists the binding's last processed change, normally LoadedProject.PollCursorStore. Use
	// pollingtrigger.NewMemoryCursorStore only when changes made while the process is stopped may be skipped.
	Cursor pollingtrigger.CursorStore
	// Target receives each changed record. A target wrapped by provider.NewDurableTriggerTarget records it
	// before the cursor moves past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[RecordChangedEvent]
}

// RecordChangedTriggerRunnerConfig configures every recordChanged binding of one Airtable connection.
type RecordChangedTriggerRunnerConfig struct {
	// Connection is the Airtable connection whose pollInterval and pollPageSize settings apply.
	Connection Connection
	// Routes are polled in this order whenever several are due; at least one is required.
	Routes []RecordChangedTriggerRoute
	// RequestLimiter optionally paces every poll request, for example to share Airtable's 50 requests per second per token.
	RequestLimiter pollingtrigger.RequestLimiter
}

// RecordChangedTriggerRunner polls each recordChanged binding's table from one goroutine.
type RecordChangedTriggerRunner struct {
	runner *pollingtrigger.Runner
}

// NewRecordChangedTriggerRunner validates every route and builds one poll runner for the connection. It makes
// no provider request and reads no storage.
//
// Each binding first reads the base schema to find the Last modified time field's current name, then
// seeds its cursor at the table's most recent change, so existing records never start Flows. Later polls
// list up to pollPageSize records modified since the cursor, oldest change first, with one listRecords
// request, and deliver each one before the cursor commits. The cursor stores the latest delivered
// modification time and the records delivered in the five seconds before it, so a change that becomes
// visible slightly late is still delivered and a record is delivered once per change. A long scan
// resumes from its stored Airtable offset. A record changed again later is delivered again. Delivery is at least once, and each
// event ID is a pure function of the record ID and its modification time.
func NewRecordChangedTriggerRunner(config RecordChangedTriggerRunnerConfig) (*RecordChangedTriggerRunner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.Routes) == 0 {
		return nil, errors.New("Airtable recordChanged Trigger routes are required")
	}
	client := config.Connection.client
	pollers := make([]pollingtrigger.Poller, 0, len(config.Routes))
	for _, route := range config.Routes {
		if strings.TrimSpace(route.BindingName) == "" || route.Target == nil || route.Cursor == nil {
			return nil, errors.New("Airtable recordChanged binding name, cursor, and target are required")
		}
		if err := route.Configuration.Validate(); err != nil {
			return nil, fmt.Errorf("Airtable recordChanged binding %q: %w", route.BindingName, err)
		}
		binding := sdkgo.TriggerBindingRef{Connection: config.Connection.reference, Trigger: RecordChangedTriggerDefinition.Trigger, Name: route.BindingName}
		poller, err := pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[RecordChangedEvent]{
			Definition: RecordChangedTriggerDefinition, Binding: binding, Cursor: route.Cursor,
			Fetcher: client.newRecordChangedFetcher(binding, route.Configuration), Target: route.Target,
		})
		if err != nil {
			return nil, fmt.Errorf("Airtable recordChanged binding %q: %w", route.BindingName, err)
		}
		pollers = append(pollers, poller)
	}
	runner, err := pollingtrigger.NewRunner(pollingtrigger.RunnerConfig{
		Connection: config.Connection.reference, Schedule: client.recordChangedPollSchedule(), Pollers: pollers,
		RequestLimiter: config.RequestLimiter, Logger: client.logger,
	})
	if err != nil {
		return nil, err
	}
	return &RecordChangedTriggerRunner{runner: runner}, nil
}

// Run replays each durable target's pending records, seeds missing cursors, and then polls every binding until
// ctx ends. A failed poll backs off exponentially up to five minutes, and HTTP 429 pauses every binding of
// the connection for at least Airtable's documented 30-second wait. Run returns ctx.Err() on cancellation, or an error for a
// stored cursor document that can never be decoded. Records go to the connection's WithLogger logger.
func (runner *RecordChangedTriggerRunner) Run(ctx context.Context) error {
	return runner.runner.Run(ctx)
}

// ProjectRecordChangedTriggerRoute binds one recordChanged binding saved in the project configuration to its target.
type ProjectRecordChangedTriggerRoute struct {
	// BindingName names the binding whose base, table, and field Dex Web saved.
	BindingName string
	// Target receives each changed record after the binding's durable project inbox records it.
	Target sdkgo.TriggerTarget[RecordChangedEvent]
}

// NewProjectRecordChangedTriggerRunner opens connectionName from the loaded project, loads each route's saved
// binding configuration, and builds a durable runner: each binding's cursor lives in project storage beside
// its Trigger inbox, and every change is recorded in that inbox before the cursor commits past it. A restarted
// or replaced replica therefore neither misses nor replays a change. WithLogger also applies to the inboxes.
func NewProjectRecordChangedTriggerRunner(
	project *projectconfig.LoadedProject,
	connectionName string,
	routes []ProjectRecordChangedTriggerRoute,
	options ...Option,
) (*RecordChangedTriggerRunner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableRecordChangedTriggerRunner(connection, project.Configuration, project.TriggerInbox,
		func(key projectconfig.PollCursorKey) (pollingtrigger.CursorStore, error) {
			return project.PollCursorStore(key)
		}, routes)
}

// newDurableRecordChangedTriggerRunner opens each route's inbox and cursor with the supplied project storage.
func newDurableRecordChangedTriggerRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	openInbox func(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error),
	openCursor func(projectconfig.PollCursorKey) (pollingtrigger.CursorStore, error),
	routes []ProjectRecordChangedTriggerRoute,
) (*RecordChangedTriggerRunner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := RecordChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := RecordChangedTriggerRunnerConfig{Connection: connection}
	for _, route := range routes {
		if route.Target == nil {
			return nil, fmt.Errorf("Airtable recordChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration RecordChangedTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration); err != nil {
			return nil, fmt.Errorf("Airtable recordChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.Routes = append(runnerConfig.Routes, RecordChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Cursor: cursor, Target: target,
		})
	}
	return NewRecordChangedTriggerRunner(runnerConfig)
}
