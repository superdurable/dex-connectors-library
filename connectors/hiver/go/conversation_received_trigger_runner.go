// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

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

// ConversationReceivedTriggerRoute binds one conversationReceived binding's configuration and cursor to its
// application target.
type ConversationReceivedTriggerRoute struct {
	// BindingName names the configured Trigger binding; it is unique within one runner.
	BindingName string
	// Configuration selects the polled shared inbox.
	Configuration ConversationReceivedTriggerConfiguration
	// Cursor persists the binding's tracked conversation IDs and sweep position, normally
	// LoadedProject.PollCursorStore. Use pollingtrigger.NewMemoryCursorStore only when conversations that
	// arrive while the process is stopped may be skipped.
	Cursor pollingtrigger.CursorStore
	// Target receives each new conversation. A target wrapped by provider.NewDurableTriggerTarget records
	// the conversation before the cursor moves past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[ConversationReceivedEvent]
}

// ConversationReceivedTriggerRunnerConfig configures every conversationReceived binding of one Hiver
// connection.
type ConversationReceivedTriggerRunnerConfig struct {
	// Connection is the Hiver connection whose pollInterval, pollPagesPerPoll, and request spacing apply.
	Connection Connection
	// Routes are polled in this order whenever several are due; at least one is required.
	Routes []ConversationReceivedTriggerRoute
	// RequestLimiter optionally gates every poll request in addition to the connection's request spacing,
	// for example to share the account's 5000 daily requests with other work.
	RequestLimiter pollingtrigger.RequestLimiter
}

// ConversationReceivedTriggerRunner polls each conversationReceived binding's shared inbox from one goroutine.
type ConversationReceivedTriggerRunner struct {
	runner *pollingtrigger.Runner
}

// NewConversationReceivedTriggerRunner validates every route and builds one poll runner for the connection.
// It makes no provider request and reads no storage.
//
// A binding's first polls walk the inbox's whole conversation list, 10 pages of 100 at a time and
// continuing at once, and record every listed conversation ID without delivering it. After that baseline,
// each poll reads the list's first page plus up to pollPagesPerPoll-1 pages of a sweep that walks the rest
// of the list across polls, and delivers each conversation ID the binding has not tracked, in list order,
// before the cursor commits. Hiver documents no order for the list, so a conversation is found on the next
// poll when Hiver lists it on the first page, and otherwise by the end of the current sweep. IDs that two
// complete sweeps in a row never list, such as deleted conversations, stop being tracked, so a conversation
// that a list change shifts past one sweep's page boundary is not delivered again. A rejected sweep page token
// restarts the sweep without a gap. Delivery is at least once, and each event ID is a pure function of the
// inbox and conversation IDs. An inbox listing more than MaxTrackedConversations pauses its binding.
func NewConversationReceivedTriggerRunner(config ConversationReceivedTriggerRunnerConfig) (*ConversationReceivedTriggerRunner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.Routes) == 0 {
		return nil, errors.New("Hiver conversationReceived Trigger routes are required")
	}
	client := config.Connection.client
	definition := ConversationReceivedTriggerDefinition
	pollers := make([]pollingtrigger.Poller, 0, len(config.Routes))
	for _, route := range config.Routes {
		if strings.TrimSpace(route.BindingName) == "" || route.Target == nil || route.Cursor == nil {
			return nil, errors.New("Hiver conversationReceived binding name, cursor, and target are required")
		}
		if err := route.Configuration.Validate(); err != nil {
			return nil, fmt.Errorf("Hiver conversationReceived binding %q: %w", route.BindingName, err)
		}
		binding := sdkgo.TriggerBindingRef{Connection: config.Connection.reference, Trigger: definition.Trigger, Name: route.BindingName}
		poller, err := pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[ConversationReceivedEvent]{
			Definition: definition, Binding: binding, Cursor: route.Cursor,
			Fetcher: client.newConversationReceivedFetcher(binding, route.Configuration), Target: route.Target,
			Snapshot: conversationSnapshotOptions(),
		})
		if err != nil {
			return nil, fmt.Errorf("Hiver conversationReceived binding %q: %w", route.BindingName, err)
		}
		pollers = append(pollers, poller)
	}
	runner, err := pollingtrigger.NewRunner(pollingtrigger.RunnerConfig{
		Connection: config.Connection.reference, Schedule: client.conversationReceivedPollSchedule(), Pollers: pollers,
		RequestLimiter: config.RequestLimiter, Logger: client.logger,
	})
	if err != nil {
		return nil, err
	}
	return &ConversationReceivedTriggerRunner{runner: runner}, nil
}

// Run replays each durable target's pending conversations, starts missing baselines, and then polls every
// binding until ctx ends. A failed poll backs off exponentially up to 30 minutes, or pollInterval when
// longer, and HTTP 429 pauses every binding of the connection for at least its Retry-After. Run returns
// ctx.Err() on cancellation, or an error for a stored cursor document that can never be decoded. Records
// go to the connection's WithLogger logger.
func (runner *ConversationReceivedTriggerRunner) Run(ctx context.Context) error {
	return runner.runner.Run(ctx)
}

// ProjectConversationReceivedTriggerRoute binds one conversationReceived binding saved in the project
// configuration to its target.
type ProjectConversationReceivedTriggerRoute struct {
	// BindingName names the binding whose shared inbox Dex Web saved.
	BindingName string
	// Target receives each new conversation after the binding's durable project inbox records it.
	Target sdkgo.TriggerTarget[ConversationReceivedEvent]
}

// NewProjectConversationReceivedTriggerRunner opens connectionName from the loaded project, loads each
// route's saved binding configuration, and builds a durable runner: each binding's cursor lives in project
// storage beside its Trigger inbox, and every conversation is recorded in that inbox before the cursor
// commits past it. A restarted or replaced replica therefore neither misses nor replays a conversation.
// WithLogger also applies to the inboxes.
func NewProjectConversationReceivedTriggerRunner(
	project *projectconfig.LoadedProject,
	connectionName string,
	routes []ProjectConversationReceivedTriggerRoute,
	options ...Option,
) (*ConversationReceivedTriggerRunner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableConversationReceivedTriggerRunner(connection, project.Configuration, project.TriggerInbox,
		func(key projectconfig.PollCursorKey) (pollingtrigger.CursorStore, error) {
			return project.PollCursorStore(key)
		}, routes)
}

// newDurableConversationReceivedTriggerRunner opens each route's inbox and cursor with the supplied storage.
func newDurableConversationReceivedTriggerRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	openInbox func(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error),
	openCursor func(projectconfig.PollCursorKey) (pollingtrigger.CursorStore, error),
	routes []ProjectConversationReceivedTriggerRoute,
) (*ConversationReceivedTriggerRunner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := ConversationReceivedTriggerDefinition.Trigger.TriggerName
	runnerConfig := ConversationReceivedTriggerRunnerConfig{Connection: connection}
	for _, route := range routes {
		if route.Target == nil {
			return nil, fmt.Errorf("Hiver conversationReceived binding %q target is required", route.BindingName)
		}
		var bindingConfiguration ConversationReceivedTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration); err != nil {
			return nil, fmt.Errorf("Hiver conversationReceived binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.Routes = append(runnerConfig.Routes, ConversationReceivedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Cursor: cursor, Target: target,
		})
	}
	return NewConversationReceivedTriggerRunner(runnerConfig)
}
