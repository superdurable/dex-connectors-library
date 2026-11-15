// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// MessageReceivedTriggerRoute binds one messageReceived configuration to its application target and cursor.
type MessageReceivedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the receiving address; blank receives every inbound message of the account.
	Configuration MessageReceivedTriggerConfiguration
	// Target routes each inbound message into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each message before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[MessageReceivedEvent]
	// Cursor stores the binding's DateSent watermark and delivered-message snapshot, such as
	// projectconfig.LoadedProject.PollCursorStore. Nil keeps it in memory, so every process start records a
	// new baseline and messages received while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// MessageTriggerRunnerConfig configures every Twilio Messaging Trigger binding polled through one connection.
type MessageTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// MessageReceivedRoutes lists the messageReceived bindings; at least one is required.
	MessageReceivedRoutes []MessageReceivedTriggerRoute
}

// NewMessageTriggerRunner creates one poll runner for the configured messageReceived bindings. Run replays
// each durable target, records a baseline for a binding without a cursor, then lists each binding's Messages
// each pollInterval with jitter. A cursor for another account or receiving address records a new baseline
// and logs a WARN record; messages received between the last poll and the new baseline are not delivered.
// A 429 pauses every binding of the connection for Retry-After, or backs off exponentially without it;
// other failures back off per binding up to 15 minutes. Delivery is at least once, and each message keeps
// one Trigger event ID, from its SID, across refetches.
func NewMessageTriggerRunner(config MessageTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.MessageReceivedRoutes) == 0 {
		return nil, fmt.Errorf("Twilio messageReceived routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.MessageReceivedRoutes))
	for _, route := range config.MessageReceivedRoutes {
		poller, err := newMessageReceivedPoller(config.Connection, route)
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

// ProjectMessageReceivedTriggerRoute selects one messageReceived binding saved in the project configuration.
type ProjectMessageReceivedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each inbound message into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[MessageReceivedEvent]
}

// ProjectMessageTriggerRunnerConfig selects Twilio Messaging Trigger bindings saved in the project configuration.
type ProjectMessageTriggerRunnerConfig struct {
	// MessageReceivedRoutes lists the messageReceived bindings; at least one is required.
	MessageReceivedRoutes []ProjectMessageReceivedTriggerRoute
}

// NewProjectMessageTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// receiving address (an unsaved binding receives every inbound message of the account), and creates a
// durable runner: each route records messages in its binding's project Trigger inbox before its watermark
// commits in the binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a message. The
// WithLogger option also applies to those inboxes.
func NewProjectMessageTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectMessageTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableMessageTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableMessageTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectMessageTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := MessageReceivedTriggerDefinition.Trigger.TriggerName
	runnerConfig := MessageTriggerRunnerConfig{Connection: connection}
	for _, route := range config.MessageReceivedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Twilio messageReceived binding %q target is required", route.BindingName)
		}
		var bindingConfiguration MessageReceivedTriggerConfiguration
		// An unsaved binding receives every inbound message, as a blank address does.
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if err != nil && !errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("twilio-messaging messageReceived binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.MessageReceivedRoutes = append(runnerConfig.MessageReceivedRoutes, MessageReceivedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewMessageTriggerRunner(runnerConfig)
}

func newMessageReceivedPoller(connection Connection, route MessageReceivedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, fmt.Errorf("Twilio messageReceived binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, messageReceivedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[MessageReceivedEvent]{
		Definition: MessageReceivedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: MessageReceivedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Snapshot: &inboundSnapshotOptions, Fetcher: newInboundMessageFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
	})
}

// messageReceivedTriggerSource serves the generated NewEventChangedTrigger. It has no binding name or project
// storage, so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) messageReceivedTriggerSource(connection sdkgo.ConnectionRef, configuration MessageReceivedTriggerConfiguration) sdkgo.TriggerSource[MessageReceivedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(messageReceivedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[MessageReceivedEvent]{
		Definition: MessageReceivedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: MessageReceivedTriggerDefinition.Trigger, Name: MessageReceivedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Snapshot: &inboundSnapshotOptions, Fetcher: newInboundMessageFetcher(client, connection, configuration),
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
