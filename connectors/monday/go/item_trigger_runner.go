// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// ItemChangedTriggerRoute binds one itemChanged configuration to its application target and cursor.
type ItemChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched board.
	Configuration ItemChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[ItemChangedEvent]
	// Cursor stores the binding's position and snapshot, such as projectconfig.LoadedProject.PollCursorStore.
	// Nil keeps it in memory, so every process start records a new baseline and changes made while stopped
	// are lost.
	Cursor pollingtrigger.CursorStore
}

// ItemTriggerRunnerConfig configures every monday.com Trigger binding polled through one connection.
type ItemTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// ItemChangedRoutes lists the itemChanged bindings; at least one is required.
	ItemChangedRoutes []ItemChangedTriggerRoute
}

// NewItemTriggerRunner creates one poll runner for the configured itemChanged bindings. Run replays each
// durable target, records a baseline for a binding without a cursor, then polls every binding each
// pollInterval with jitter. A rate-limit error pauses every binding of the connection for monday.com's
// Retry-After or retry_in_seconds, and DAILY_LIMIT_EXCEEDED pauses it until midnight UTC; other failures
// back off per binding. More changes than one 10,000-item scan can read record a new baseline and log a
// WARN record. Delivery is at least once, and each item version keeps one Trigger event ID.
func NewItemTriggerRunner(config ItemTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.ItemChangedRoutes) == 0 {
		return nil, errors.New("monday.com itemChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.ItemChangedRoutes))
	for _, route := range config.ItemChangedRoutes {
		poller, err := newItemChangedPoller(config.Connection, route)
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

// ProjectItemChangedTriggerRoute selects one itemChanged binding saved in the project configuration.
type ProjectItemChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[ItemChangedEvent]
}

// ProjectItemTriggerRunnerConfig selects monday.com Trigger bindings saved in the project configuration.
type ProjectItemTriggerRunnerConfig struct {
	// ItemChangedRoutes lists the itemChanged bindings; at least one is required.
	ItemChangedRoutes []ProjectItemChangedTriggerRoute
}

// NewProjectItemTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// board, and creates a durable runner: each route records changes in its binding's project Trigger inbox
// before its position commits in the binding's project poll cursor, so a restarted or replaced replica
// neither misses nor repeats a change. A binding without a saved board ID is an error. The WithLogger
// option also applies to those inboxes.
func NewProjectItemTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectItemTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableItemTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableItemTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectItemTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := ItemChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := ItemTriggerRunnerConfig{Connection: connection}
	for _, route := range config.ItemChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("monday.com itemChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration ItemChangedTriggerConfiguration
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("monday itemChanged binding %q has no saved board; enter its board ID in Dex Web and restart", route.BindingName)
		}
		if err != nil {
			return nil, fmt.Errorf("monday itemChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.ItemChangedRoutes = append(runnerConfig.ItemChangedRoutes, ItemChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewItemTriggerRunner(runnerConfig)
}

func newItemChangedPoller(connection Connection, route ItemChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, errors.New("monday.com itemChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, itemChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	snapshot := itemChangedSnapshotOptions
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[ItemChangedEvent]{
		Definition: ItemChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: ItemChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newItemChangeFetcher(connection.client, connection.reference, route.Configuration),
		Target: route.Target, Snapshot: &snapshot,
	})
}

// itemChangedTriggerSource serves the generated NewItemChangedTrigger. It has no project storage, so it
// keeps the cursor in memory and records a new baseline on every start.
func (client *Client) itemChangedTriggerSource(connection sdkgo.ConnectionRef, configuration ItemChangedTriggerConfiguration) sdkgo.TriggerSource[ItemChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(itemChangedTriggerConfigurationError(err))
	}
	snapshot := itemChangedSnapshotOptions
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[ItemChangedEvent]{
		Definition: ItemChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: ItemChangedTriggerDefinition.Trigger, Name: ItemChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newItemChangeFetcher(client, connection, configuration),
		Snapshot: &snapshot,
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
