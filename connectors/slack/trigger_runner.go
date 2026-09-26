// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// ChannelThreadCreatedTriggerRoute binds one root-message configuration to its application target.
type ChannelThreadCreatedTriggerRoute struct {
	BindingName   string
	Configuration ChannelThreadCreatedTriggerConfiguration
	Target        sdkgo.TriggerTarget[MessageEvent]
}

// ThreadReplyCreatedTriggerRoute binds one reply-message configuration to its application target.
type ThreadReplyCreatedTriggerRoute struct {
	BindingName   string
	Configuration ThreadReplyCreatedTriggerConfiguration
	Target        sdkgo.TriggerTarget[MessageEvent]
}

// MessageTriggerRunnerConfig configures Slack message Triggers that share one Socket Mode connection.
type MessageTriggerRunnerConfig struct {
	Connection                 Connection
	ChannelThreadCreatedRoutes []ChannelThreadCreatedTriggerRoute
	ThreadReplyCreatedRoutes   []ThreadReplyCreatedTriggerRoute
}

// MessageTriggerRunner receives Slack messages through one Socket Mode connection and dispatches matching routes.
type MessageTriggerRunner struct {
	routes []messageTriggerRoute
}

// NewMessageTriggerRunner creates one runner for all configured Slack message Trigger routes.
func NewMessageTriggerRunner(config MessageTriggerRunnerConfig) (*MessageTriggerRunner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	routes := make([]messageTriggerRoute, 0, len(config.ChannelThreadCreatedRoutes)+len(config.ThreadReplyCreatedRoutes))
	bindingKeys := make(map[string]bool, cap(routes))
	for _, route := range config.ChannelThreadCreatedRoutes {
		if err := validateMessageTriggerRoute("channelThreadCreated", route.BindingName, route.Target, bindingKeys); err != nil {
			return nil, err
		}
		if err := route.Configuration.Validate(); err != nil {
			return nil, err
		}
		source := config.Connection.client.channelThreadCreatedTriggerSource(config.Connection.reference, route.Configuration)
		source.bindingName = route.BindingName
		routes = append(routes, messageTriggerRoute{source: source, target: route.Target})
	}
	for _, route := range config.ThreadReplyCreatedRoutes {
		if err := validateMessageTriggerRoute("threadReplyCreated", route.BindingName, route.Target, bindingKeys); err != nil {
			return nil, err
		}
		if err := route.Configuration.Validate(); err != nil {
			return nil, err
		}
		source := config.Connection.client.threadReplyCreatedTriggerSource(config.Connection.reference, route.Configuration)
		source.bindingName = route.BindingName
		routes = append(routes, messageTriggerRoute{source: source, target: route.Target})
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("Slack message Trigger routes are required")
	}
	return &MessageTriggerRunner{routes: routes}, nil
}

// Run replays each route's durable inbox in order, roots first, retrying failures until the context ends,
// then receives and dispatches Slack message events until the context ends or the runner fails. Before
// each reconnect it replays the inboxes again, so an event persisted on a failed connection is delivered
// before any envelope read on the next one.
//
// Run logs through the connection's WithLogger logger: INFO when a Socket Mode connection opens, WARN with
// the next delay when one fails, DEBUG for every ignored message with its reason, and the sdkgo delivery
// records (skips, retries with their backoff delay, and recoveries) with the route's binding.
func (runner *MessageTriggerRunner) Run(ctx context.Context) error {
	if err := replayMessageTriggerRoutes(ctx, runner.routes); err != nil {
		return err
	}
	return runMessageTriggerRoutes(ctx, runner.routes)
}

// LocalChannelThreadCreatedTriggerRoute binds one stored root-message configuration to its application target.
type LocalChannelThreadCreatedTriggerRoute struct {
	BindingName string
	Target      sdkgo.TriggerTarget[MessageEvent]
}

// LocalThreadReplyCreatedTriggerRoute binds one stored reply-message configuration to its application target.
type LocalThreadReplyCreatedTriggerRoute struct {
	BindingName string
	Target      sdkgo.TriggerTarget[MessageEvent]
}

// LocalMessageTriggerRunnerConfig selects stored Slack message Trigger bindings.
type LocalMessageTriggerRunnerConfig struct {
	ChannelThreadCreatedRoutes []LocalChannelThreadCreatedTriggerRoute
	ThreadReplyCreatedRoutes   []LocalThreadReplyCreatedTriggerRoute
}

// NewLocalMessageTriggerRunner loads stored bindings and creates one durable Socket Mode runner. The
// WithLogger option also applies to the durable inboxes it creates.
func NewLocalMessageTriggerRunner(
	store *localconfig.Store,
	connectionName string,
	config LocalMessageTriggerRunnerConfig,
	options ...Option,
) (*MessageTriggerRunner, error) {
	connection, err := NewLocalConnection(store, connectionName, options...)
	if err != nil {
		return nil, err
	}
	runnerConfig := MessageTriggerRunnerConfig{Connection: connection}
	for _, route := range config.ChannelThreadCreatedRoutes {
		var configuration ChannelThreadCreatedTriggerConfiguration
		if err := store.DecodeTriggerConfiguration(ConnectorID, connectionName, "channelThreadCreated", route.BindingName, &configuration); err != nil {
			return nil, err
		}
		target, err := localconfig.NewDurableTriggerTarget(store, ConnectorID, connectionName, "channelThreadCreated", route.BindingName, route.Target,
			localconfig.WithTriggerLogger(connection.client.logger))
		if err != nil {
			return nil, err
		}
		runnerConfig.ChannelThreadCreatedRoutes = append(runnerConfig.ChannelThreadCreatedRoutes, ChannelThreadCreatedTriggerRoute{
			BindingName: route.BindingName, Configuration: configuration, Target: target,
		})
	}
	for _, route := range config.ThreadReplyCreatedRoutes {
		var configuration ThreadReplyCreatedTriggerConfiguration
		if err := store.DecodeTriggerConfiguration(ConnectorID, connectionName, "threadReplyCreated", route.BindingName, &configuration); err != nil {
			return nil, err
		}
		target, err := localconfig.NewDurableTriggerTarget(store, ConnectorID, connectionName, "threadReplyCreated", route.BindingName, route.Target,
			localconfig.WithTriggerLogger(connection.client.logger))
		if err != nil {
			return nil, err
		}
		runnerConfig.ThreadReplyCreatedRoutes = append(runnerConfig.ThreadReplyCreatedRoutes, ThreadReplyCreatedTriggerRoute{
			BindingName: route.BindingName, Configuration: configuration, Target: target,
		})
	}
	return NewMessageTriggerRunner(runnerConfig)
}

func validateMessageTriggerRoute(
	triggerName string,
	bindingName string,
	target sdkgo.TriggerTarget[MessageEvent],
	bindingKeys map[string]bool,
) error {
	trimmedBindingName := strings.TrimSpace(bindingName)
	if trimmedBindingName == "" || target == nil {
		return fmt.Errorf("Slack message Trigger binding name and target are required")
	}
	bindingKey := triggerName + "\x00" + trimmedBindingName
	if bindingKeys[bindingKey] {
		return fmt.Errorf("Slack message Trigger binding name %q is duplicated", trimmedBindingName)
	}
	bindingKeys[bindingKey] = true
	return nil
}
