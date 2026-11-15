// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// ConversationCreatedTriggerRoute binds one conversationCreated configuration to its application target and cursor.
type ConversationCreatedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched channel; blank watches every channel of the brand.
	Configuration ConversationCreatedTriggerConfiguration
	// Target routes each new conversation into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each one before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[ConversationCreatedEvent]
	// Cursor stores the binding's creation-time cursor, such as projectconfig.LoadedProject.PollCursorStore.
	// Nil keeps it in memory, so every process start records a new baseline and conversations created while
	// stopped are not delivered.
	Cursor pollingtrigger.CursorStore
}

// ConversationTriggerRunnerConfig configures every Re:amaze Trigger binding polled through one connection.
type ConversationTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// ConversationCreatedRoutes lists the conversationCreated bindings; at least one is required.
	ConversationCreatedRoutes []ConversationCreatedTriggerRoute
}

// NewConversationTriggerRunner creates one poll runner for the configured conversationCreated bindings. Run
// replays each durable target, records a baseline for a binding without a cursor, then polls every binding
// each pollInterval with jitter. A cursor for another brand or channel records a new baseline and logs a
// WARN record. A 429 pauses every binding of the connection for Retry-After, or for one minute, Re:amaze's
// documented limit window, without it; other failures back off per binding up to 15 minutes. Delivery is
// at least once, and each conversation keeps one Trigger event ID across refetches.
func NewConversationTriggerRunner(config ConversationTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.ConversationCreatedRoutes) == 0 {
		return nil, fmt.Errorf("Re:amaze conversationCreated routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.ConversationCreatedRoutes))
	for _, route := range config.ConversationCreatedRoutes {
		poller, err := newConversationCreatedPoller(config.Connection, route)
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

// ProjectConversationCreatedTriggerRoute selects one conversationCreated binding saved in the project configuration.
type ProjectConversationCreatedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each new conversation into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[ConversationCreatedEvent]
}

// ProjectConversationTriggerRunnerConfig selects Re:amaze Trigger bindings saved in the project configuration.
type ProjectConversationTriggerRunnerConfig struct {
	// ConversationCreatedRoutes lists the conversationCreated bindings; at least one is required.
	ConversationCreatedRoutes []ProjectConversationCreatedTriggerRoute
}

// NewProjectConversationTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// channel (an unsaved binding watches every channel), and creates a durable runner: each route records new
// conversations in its binding's project Trigger inbox before its cursor commits in the binding's project
// poll cursor, so a restarted or replaced replica neither misses nor repeats a conversation created within
// ConversationCreatedLateIndexWindow of the newest one seen. The WithLogger option also applies to those
// inboxes.
func NewProjectConversationTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectConversationTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableConversationTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableConversationTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectConversationTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := ConversationCreatedTriggerDefinition.Trigger.TriggerName
	runnerConfig := ConversationTriggerRunnerConfig{Connection: connection}
	for _, route := range config.ConversationCreatedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Re:amaze conversationCreated binding %q target is required", route.BindingName)
		}
		var bindingConfiguration ConversationCreatedTriggerConfiguration
		// An unsaved binding watches every channel, as a blank entry does.
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if err != nil && !errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("reamaze conversationCreated binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.ConversationCreatedRoutes = append(runnerConfig.ConversationCreatedRoutes, ConversationCreatedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewConversationTriggerRunner(runnerConfig)
}

func newConversationCreatedPoller(connection Connection, route ConversationCreatedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, fmt.Errorf("Re:amaze conversationCreated binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, conversationCreatedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[ConversationCreatedEvent]{
		Definition: ConversationCreatedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: ConversationCreatedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newConversationCreatedFetcher(connection.client, connection.reference, route.BindingName, route.Configuration), Target: route.Target,
	})
}

// conversationCreatedTriggerSource serves the generated NewConversationCreatedTrigger. It has no binding name or project
// storage, so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) conversationCreatedTriggerSource(connection sdkgo.ConnectionRef, configuration ConversationCreatedTriggerConfiguration) sdkgo.TriggerSource[ConversationCreatedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(conversationCreatedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[ConversationCreatedEvent]{
		Definition: ConversationCreatedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: ConversationCreatedTriggerDefinition.Trigger, Name: ConversationCreatedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newConversationCreatedFetcher(client, connection, ConversationCreatedTriggerDefinition.Trigger.TriggerName, configuration),
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
