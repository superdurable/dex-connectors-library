// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// CardChangedTriggerRoute binds one cardChanged configuration to its application target and cursor.
type CardChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched board.
	Configuration CardChangedTriggerConfiguration
	// Target routes each card action into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each action before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[CardChangedEvent]
	// Cursor stores the binding's position, such as projectconfig.LoadedProject.PollCursorStore. Nil keeps
	// it in memory, so every process start records a new baseline and actions made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// CardTriggerRunnerConfig configures every Trello Trigger binding polled through one connection.
type CardTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// CardChangedRoutes lists the cardChanged bindings; at least one is required.
	CardChangedRoutes []CardChangedTriggerRoute
}

// NewCardTriggerRunner creates one poll runner for the configured cardChanged bindings. Run replays each
// durable target, records a baseline for a binding without a cursor, then polls every binding each
// pollInterval with jitter. One poll re-reads the last minute of the board's action history, skips the
// action IDs it already delivered, and delivers the rest oldest first, 500 per page. A backlog over
// 10,000 actions, or a 400 that rejects since or before, records a new baseline and logs a WARN record;
// actions in that gap are not delivered. A 429 pauses every binding of the connection for Retry-After, or
// one 10-second Trello rate-limit window without it; other failures back off per binding up to 15
// minutes. Delivery is at least once, and each action keeps one Trigger event ID across refetches.
func NewCardTriggerRunner(config CardTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.CardChangedRoutes) == 0 {
		return nil, fmt.Errorf("Trello cardChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.CardChangedRoutes))
	for _, route := range config.CardChangedRoutes {
		poller, err := newCardChangedPoller(config.Connection, route)
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

// ProjectCardChangedTriggerRoute selects one cardChanged binding saved in the project configuration.
type ProjectCardChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each card action into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[CardChangedEvent]
}

// ProjectCardTriggerRunnerConfig selects Trello Trigger bindings saved in the project configuration.
type ProjectCardTriggerRunnerConfig struct {
	// CardChangedRoutes lists the cardChanged bindings; at least one is required.
	CardChangedRoutes []ProjectCardChangedTriggerRoute
}

// NewProjectCardTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// board, and creates a durable runner: each route records card actions in its binding's project Trigger
// inbox before its cursor commits in the binding's project poll cursor, so a restarted or replaced
// replica neither misses nor repeats an action. A binding without a saved board is an error. The
// WithLogger option also applies to those inboxes.
func NewProjectCardTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectCardTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableCardTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableCardTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectCardTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := CardChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := CardTriggerRunnerConfig{Connection: connection}
	for _, route := range config.CardChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Trello cardChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration CardChangedTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration); err != nil {
			return nil, fmt.Errorf("trello cardChanged binding %q configuration (save the watched board in Dex Web): %w", route.BindingName, err)
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
		runnerConfig.CardChangedRoutes = append(runnerConfig.CardChangedRoutes, CardChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewCardTriggerRunner(runnerConfig)
}

func newCardChangedPoller(connection Connection, route CardChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, fmt.Errorf("Trello cardChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, cardChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[CardChangedEvent]{
		Definition: CardChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: CardChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newCardActionFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
	})
}

// cardChangedTriggerSource serves the generated NewCardChangedTrigger. It has no project storage, so it
// keeps the cursor in memory and records a new baseline on every start.
func (client *Client) cardChangedTriggerSource(connection sdkgo.ConnectionRef, configuration CardChangedTriggerConfiguration) sdkgo.TriggerSource[CardChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(cardChangedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[CardChangedEvent]{
		Definition: CardChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: CardChangedTriggerDefinition.Trigger, Name: CardChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newCardActionFetcher(client, connection, configuration),
	}, client.pollSchedule())
	if err != nil {
		panic(err)
	}
	return source
}

func (client *Client) pollSchedule() pollingtrigger.PollSchedule {
	// A zero MaxBackoff uses the kit's cap.
	return pollingtrigger.PollSchedule{Interval: client.pollInterval}
}
