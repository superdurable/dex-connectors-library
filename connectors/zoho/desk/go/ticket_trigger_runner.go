// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// TicketChangedTriggerRoute binds one ticketChanged configuration to its application target and cursor.
type TicketChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched department.
	Configuration TicketChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[TicketChangedEvent]
	// Cursor stores the binding's search position and recent ticket versions, such as
	// projectconfig.LoadedProject.PollCursorStore. Nil keeps it in memory, so every process start records
	// a new baseline and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// TicketTriggerRunnerConfig configures every Zoho Desk Trigger binding polled through one connection.
type TicketTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// TicketChangedRoutes lists the ticketChanged bindings; at least one is required.
	TicketChangedRoutes []TicketChangedTriggerRoute
}

// NewTicketTriggerRunner creates one poll runner for the configured ticketChanged bindings. Run replays
// each durable target, records the tickets modified in the last TicketSearchIndexLookback as a baseline
// for a binding without a cursor, then polls every binding each pollInterval with jitter. Each poll reads
// Zoho Desk ticket search in ascending modified-time order, at most 100 tickets per request and 10
// requests per poll. A 429 pauses every binding of the connection for Retry-After, or backs off
// exponentially without it; other failures back off per binding up to 15 minutes. A search that rejects
// the cursor's time range records a new baseline and logs a WARN record. Delivery is at least once, and
// each ticket version keeps one Trigger event ID across refetches.
func NewTicketTriggerRunner(config TicketTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.TicketChangedRoutes) == 0 {
		return nil, errors.New("Zoho Desk ticketChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.TicketChangedRoutes))
	for _, route := range config.TicketChangedRoutes {
		poller, err := newTicketChangedPoller(config.Connection, route)
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

// ProjectTicketChangedTriggerRoute selects one ticketChanged binding saved in the project configuration.
type ProjectTicketChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[TicketChangedEvent]
}

// ProjectTicketTriggerRunnerConfig selects Zoho Desk Trigger bindings saved in the project configuration.
type ProjectTicketTriggerRunnerConfig struct {
	// TicketChangedRoutes lists the ticketChanged bindings; at least one is required.
	TicketChangedRoutes []ProjectTicketChangedTriggerRoute
}

// NewProjectTicketTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// department (an unsaved binding watches every department), and creates a durable runner: each route
// records changes in its binding's project Trigger inbox before its cursor commits in the binding's
// project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. The
// WithLogger option also applies to those inboxes.
func NewProjectTicketTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectTicketTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableTicketTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableTicketTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectTicketTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := TicketChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := TicketTriggerRunnerConfig{Connection: connection}
	for _, route := range config.TicketChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Zoho Desk ticketChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration TicketChangedTriggerConfiguration
		// An unsaved binding watches every department, as a blank picker does.
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if err != nil && !errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("zoho-desk ticketChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.TicketChangedRoutes = append(runnerConfig.TicketChangedRoutes, TicketChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewTicketTriggerRunner(runnerConfig)
}

func newTicketChangedPoller(connection Connection, route TicketChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, errors.New("Zoho Desk ticketChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, ticketChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	snapshot := ticketPollSnapshotOptions
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[TicketChangedEvent]{
		Definition: TicketChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: TicketChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newTicketChangeFetcher(connection.client, connection.reference, route.Configuration),
		Target: route.Target, Snapshot: &snapshot,
	})
}

// ticketChangedTriggerSource serves the generated NewTicketChangedTrigger. It has no project storage, so
// it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) ticketChangedTriggerSource(connection sdkgo.ConnectionRef, configuration TicketChangedTriggerConfiguration) sdkgo.TriggerSource[TicketChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(ticketChangedTriggerConfigurationError(err))
	}
	snapshot := ticketPollSnapshotOptions
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[TicketChangedEvent]{
		Definition: TicketChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: TicketChangedTriggerDefinition.Trigger, Name: TicketChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newTicketChangeFetcher(client, connection, configuration),
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
