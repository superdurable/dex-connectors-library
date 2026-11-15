// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// DatabaseRowChangedTriggerRoute binds one databaseRowChanged configuration to its application target and cursor.
type DatabaseRowChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched data source; it is required.
	Configuration DatabaseRowChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[DatabaseRowChangedEvent]
	// Cursor stores the binding's last-edited-time cursor, such as projectconfig.LoadedProject.PollCursorStore. Nil keeps
	// it in memory, so every process start records a new baseline and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// DatabaseRowTriggerRunnerConfig configures every Notion databaseRowChanged binding polled through one connection.
type DatabaseRowTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// DatabaseRowChangedRoutes lists the databaseRowChanged bindings; at least one is required.
	DatabaseRowChangedRoutes []DatabaseRowChangedTriggerRoute
}

// NewDatabaseRowTriggerRunner creates one poll runner for the configured databaseRowChanged bindings. Run
// replays each durable target, records a baseline for a binding without a cursor, then queries every
// binding's data source each pollInterval with jitter, at most pollPageSize rows per request and ten
// requests per poll. A 429 rate_limited or 529 service_overload pauses every binding of the connection for
// Notion's Retry-After seconds, or backs off exponentially without it; a stale start cursor restarts the
// query, a cursor for another data source records a new baseline, and other failures, such as a data
// source that is no longer shared, back off per binding up to 15 minutes. Delivery is at least once, and
// each row version keeps one Trigger event ID across refetches.
func NewDatabaseRowTriggerRunner(config DatabaseRowTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.DatabaseRowChangedRoutes) == 0 {
		return nil, fmt.Errorf("Notion databaseRowChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.DatabaseRowChangedRoutes))
	for _, route := range config.DatabaseRowChangedRoutes {
		poller, err := newDatabaseRowChangedPoller(config.Connection, route)
		if err != nil {
			return nil, err
		}
		pollers = append(pollers, poller)
	}
	return pollingtrigger.NewRunner(pollingtrigger.RunnerConfig{
		Connection: config.Connection.reference, Schedule: config.Connection.client.pollSchedule(),
		Pollers: pollers, Logger: config.Connection.client.logger,
	})
}

// ProjectDatabaseRowChangedTriggerRoute selects one databaseRowChanged binding saved in the project configuration.
type ProjectDatabaseRowChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[DatabaseRowChangedEvent]
}

// ProjectDatabaseRowTriggerRunnerConfig selects Notion databaseRowChanged bindings saved in the project configuration.
type ProjectDatabaseRowTriggerRunnerConfig struct {
	// DatabaseRowChangedRoutes lists the databaseRowChanged bindings; at least one is required.
	DatabaseRowChangedRoutes []ProjectDatabaseRowChangedTriggerRoute
}

// NewProjectDatabaseRowTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// data source (an unsaved binding is a configuration error), and creates a durable runner: each route
// records changes in its binding's project Trigger inbox before its cursor commits in the binding's
// project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. The
// WithLogger option also applies to those inboxes.
func NewProjectDatabaseRowTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectDatabaseRowTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableDatabaseRowTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableDatabaseRowTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectDatabaseRowTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := DatabaseRowChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := DatabaseRowTriggerRunnerConfig{Connection: connection}
	for _, route := range config.DatabaseRowChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Notion databaseRowChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration DatabaseRowChangedTriggerConfiguration
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("notion databaseRowChanged binding %q has no saved data source; pick one in Dex Web and restart", route.BindingName)
		}
		if err != nil {
			return nil, fmt.Errorf("notion databaseRowChanged binding %q configuration: %w", route.BindingName, err)
		}
		key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: connection.reference.Name, TriggerName: triggerName, BindingName: route.BindingName}
		inbox, err := storage.TriggerInbox(key)
		if err != nil {
			return nil, err
		}
		target, err := provider.NewDurableTriggerTarget(inbox, key, route.Target, provider.WithTriggerLogger(connection.client.logger))
		if err != nil {
			return nil, err
		}
		cursor, err := storage.PollCursorStore(projectconfig.PollCursorKey(key))
		if err != nil {
			return nil, err
		}
		runnerConfig.DatabaseRowChangedRoutes = append(runnerConfig.DatabaseRowChangedRoutes, DatabaseRowChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewDatabaseRowTriggerRunner(runnerConfig)
}

func newDatabaseRowChangedPoller(connection Connection, route DatabaseRowChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, fmt.Errorf("Notion databaseRowChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, databaseRowChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[DatabaseRowChangedEvent]{
		Definition: DatabaseRowChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: DatabaseRowChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newDatabaseRowFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
	})
}

// databaseRowChangedTriggerSource serves the generated NewDatabaseRowChangedTrigger. It has no binding name or project
// storage, so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) databaseRowChangedTriggerSource(connection sdkgo.ConnectionRef, configuration DatabaseRowChangedTriggerConfiguration) sdkgo.TriggerSource[DatabaseRowChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(databaseRowChangedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[DatabaseRowChangedEvent]{
		Definition: DatabaseRowChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: DatabaseRowChangedTriggerDefinition.Trigger, Name: DatabaseRowChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newDatabaseRowFetcher(client, connection, configuration),
	}, client.pollSchedule())
	if err != nil {
		panic(err)
	}
	return source
}

func (client *Client) pollSchedule() pollingtrigger.PollSchedule {
	// A zero MaxBackoff uses the kit's cap, which grows with a long poll interval.
	return pollingtrigger.PollSchedule{Interval: client.pollInterval}
}
