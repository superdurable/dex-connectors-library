// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// TaskChangedTriggerRoute binds one taskChanged configuration to its application target and cursor.
type TaskChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched project; its ProjectID is required.
	Configuration TaskChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[TaskChangedEvent]
	// Cursor stores the binding's sync token, such as projectconfig.LoadedProject.PollCursorStore. Nil keeps
	// it in memory, so every process start records a new baseline and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// TaskTriggerRunnerConfig configures every Asana Trigger binding polled through one connection.
type TaskTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// TaskChangedRoutes lists the taskChanged bindings; at least one is required.
	TaskChangedRoutes []TaskChangedTriggerRoute
}

// NewTaskTriggerRunner creates one poll runner for the configured taskChanged bindings. Run replays each
// durable target, records a baseline for a binding without a cursor, then polls every binding each
// pollInterval with jitter, following up to 10 pages of 100 events per poll. An expired or invalid sync token
// (HTTP 412) records a new baseline and logs a WARN record; changes made between the last poll and the new
// baseline are not delivered. A 429 pauses every binding of the connection for Retry-After, or backs off
// exponentially without it; other failures back off per binding up to 15 minutes. Delivery is at least once
// from Asana's event stream, which Asana itself delivers at most once, and each event keeps one Trigger
// event ID across refetches of the same sync token, derived from that token and the event's immutable facts.
func NewTaskTriggerRunner(config TaskTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.TaskChangedRoutes) == 0 {
		return nil, fmt.Errorf("Asana taskChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.TaskChangedRoutes))
	for _, route := range config.TaskChangedRoutes {
		poller, err := newTaskChangedPoller(config.Connection, route)
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

// ProjectTaskChangedTriggerRoute selects one taskChanged binding saved in the project configuration.
type ProjectTaskChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[TaskChangedEvent]
}

// ProjectTaskTriggerRunnerConfig selects Asana Trigger bindings saved in the project configuration.
type ProjectTaskTriggerRunnerConfig struct {
	// TaskChangedRoutes lists the taskChanged bindings; at least one is required.
	TaskChangedRoutes []ProjectTaskChangedTriggerRoute
}

// NewProjectTaskTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// project, and creates a durable runner: each route
// records changes in its binding's project Trigger inbox before its sync token commits in the binding's
// project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. The
// WithLogger option also applies to those inboxes.
func NewProjectTaskTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectTaskTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableTaskTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableTaskTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectTaskTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := TaskChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := TaskTriggerRunnerConfig{Connection: connection}
	for _, route := range config.TaskChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Asana taskChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration TaskChangedTriggerConfiguration
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("asana taskChanged binding %q has no saved project; pick one in Dex Web and restart", route.BindingName)
		}
		if err != nil {
			return nil, fmt.Errorf("asana taskChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.TaskChangedRoutes = append(runnerConfig.TaskChangedRoutes, TaskChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewTaskTriggerRunner(runnerConfig)
}

func newTaskChangedPoller(connection Connection, route TaskChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, fmt.Errorf("Asana taskChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, taskChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[TaskChangedEvent]{
		Definition: TaskChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: TaskChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newTaskEventFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
	})
}

// taskChangedTriggerSource serves the generated NewTaskChangedTrigger. It has no binding name or project
// storage, so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) taskChangedTriggerSource(connection sdkgo.ConnectionRef, configuration TaskChangedTriggerConfiguration) sdkgo.TriggerSource[TaskChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(taskChangedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[TaskChangedEvent]{
		Definition: TaskChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: TaskChangedTriggerDefinition.Trigger, Name: TaskChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newTaskEventFetcher(client, connection, configuration),
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
