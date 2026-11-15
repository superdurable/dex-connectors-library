// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// EventChangedTriggerRoute binds one eventChanged configuration to its application target and cursor.
type EventChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched calendar.
	Configuration EventChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[EventChangedEvent]
	// Cursor stores the binding's sync token, such as projectconfig.LoadedProject.PollCursorStore. Nil keeps
	// it in memory, so every process start records a new baseline and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// EventTriggerRunnerConfig configures every Google Calendar Trigger binding polled through one connection.
type EventTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// EventChangedRoutes lists the eventChanged bindings; at least one is required.
	EventChangedRoutes []EventChangedTriggerRoute
}

// NewEventTriggerRunner creates one poll runner for the configured eventChanged bindings. Run replays each
// durable target, records a baseline for a binding without a cursor, then polls every binding each
// pollInterval with jitter. A 410 Gone records a new baseline and logs a WARN record; changes made between
// the last poll and the new baseline are not delivered. A 429 or a 403 rate-limit or quota reason pauses
// every binding of the connection for Retry-After, or backs off exponentially without it; other failures back off
// per binding up to 15 minutes. Delivery is at least
// once, and each change keeps one Trigger event ID across refetches.
func NewEventTriggerRunner(config EventTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.EventChangedRoutes) == 0 {
		return nil, fmt.Errorf("Google Calendar eventChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.EventChangedRoutes))
	for _, route := range config.EventChangedRoutes {
		poller, err := newEventChangedPoller(config.Connection, route)
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

// ProjectEventChangedTriggerRoute selects one eventChanged binding saved in the project configuration.
type ProjectEventChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[EventChangedEvent]
}

// ProjectEventTriggerRunnerConfig selects Google Calendar Trigger bindings saved in the project configuration.
type ProjectEventTriggerRunnerConfig struct {
	// EventChangedRoutes lists the eventChanged bindings; at least one is required.
	EventChangedRoutes []ProjectEventChangedTriggerRoute
}

// NewProjectEventTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// calendar (an unsaved binding watches the primary calendar), and creates a durable runner: each route
// records changes in its binding's project Trigger inbox before its sync token commits in the binding's
// project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. The
// WithLogger option also applies to those inboxes.
func NewProjectEventTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectEventTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableEventTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableEventTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectEventTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := EventChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := EventTriggerRunnerConfig{Connection: connection}
	for _, route := range config.EventChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Google Calendar eventChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration EventChangedTriggerConfiguration
		// An unsaved binding watches the primary calendar, as a blank picker does.
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if err != nil && !errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("google-calendar eventChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.EventChangedRoutes = append(runnerConfig.EventChangedRoutes, EventChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewEventTriggerRunner(runnerConfig)
}

func newEventChangedPoller(connection Connection, route EventChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, fmt.Errorf("Google Calendar eventChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, eventChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[EventChangedEvent]{
		Definition: EventChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: EventChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newEventSyncFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
	})
}

// eventChangedTriggerSource serves the generated NewEventChangedTrigger. It has no binding name or project
// storage, so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) eventChangedTriggerSource(connection sdkgo.ConnectionRef, configuration EventChangedTriggerConfiguration) sdkgo.TriggerSource[EventChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(eventChangedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[EventChangedEvent]{
		Definition: EventChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: EventChangedTriggerDefinition.Trigger, Name: EventChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newEventSyncFetcher(client, connection, configuration),
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
