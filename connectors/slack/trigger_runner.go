// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// ChannelThreadCreatedTriggerRoute binds one root-message configuration to its application target.
type ChannelThreadCreatedTriggerRoute struct {
	// BindingName names this configured Trigger binding.
	BindingName string
	// Configuration is the configuration for channel thread created trigger route.
	Configuration ChannelThreadCreatedTriggerConfiguration
	// Target routes accepted events into the application.
	Target sdkgo.TriggerTarget[MessageEvent]
}

// ThreadReplyCreatedTriggerRoute binds one reply-message configuration to its application target.
type ThreadReplyCreatedTriggerRoute struct {
	// BindingName names this configured Trigger binding.
	BindingName string
	// Configuration is the configuration for thread reply created trigger route.
	Configuration ThreadReplyCreatedTriggerConfiguration
	// Target routes accepted events into the application.
	Target sdkgo.TriggerTarget[MessageEvent]
}

// MessageTriggerRunnerConfig configures Slack message Triggers that share one Socket Mode connection.
type MessageTriggerRunnerConfig struct {
	// Connection specifies connection for message trigger runner config.
	Connection Connection
	// ChannelThreadCreatedRoutes specifies channel thread created routes for message trigger runner config.
	ChannelThreadCreatedRoutes []ChannelThreadCreatedTriggerRoute
	// ThreadReplyCreatedRoutes specifies thread reply created routes for message trigger runner config.
	ThreadReplyCreatedRoutes []ThreadReplyCreatedTriggerRoute
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

// ProjectChannelThreadCreatedTriggerRoute binds one root-message binding from the project configuration to its
// application target.
type ProjectChannelThreadCreatedTriggerRoute struct {
	// BindingName names this configured Trigger binding.
	BindingName string
	// Target routes accepted events into the application.
	Target sdkgo.TriggerTarget[MessageEvent]
}

// ProjectThreadReplyCreatedTriggerRoute binds one reply-message binding from the project configuration to its
// application target.
type ProjectThreadReplyCreatedTriggerRoute struct {
	// BindingName names this configured Trigger binding.
	BindingName string
	// Target routes accepted events into the application.
	Target sdkgo.TriggerTarget[MessageEvent]
}

// ProjectMessageTriggerRunnerConfig selects Slack message Trigger bindings from the project configuration.
type ProjectMessageTriggerRunnerConfig struct {
	// ChannelThreadCreatedRoutes specifies channel thread created routes for project message trigger runner config.
	ChannelThreadCreatedRoutes []ProjectChannelThreadCreatedTriggerRoute
	// ThreadReplyCreatedRoutes specifies thread reply created routes for project message trigger runner config.
	ThreadReplyCreatedRoutes []ProjectThreadReplyCreatedTriggerRoute
}

// durableMessageTriggerTarget wraps one binding's target so that each event is stored before its
// acknowledgement.
type durableMessageTriggerTarget func(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[MessageEvent],
) (sdkgo.TriggerTarget[MessageEvent], error)

// NewProjectMessageTriggerRunner opens connectionName and its configured bindings from the loaded project
// configuration and creates one durable Socket Mode runner. Each route's target receives its events through
// the binding's durable project inbox, which every replica of the application shares. A route whose binding
// is not configured returns an error. The WithLogger option also applies to the durable inboxes it creates.
func NewProjectMessageTriggerRunner(
	project *projectconfig.LoadedProject,
	connectionName string,
	config ProjectMessageTriggerRunnerConfig,
	options ...Option,
) (*MessageTriggerRunner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableMessageTriggerRunner(connection, project.Configuration, config, func(
		key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[MessageEvent],
	) (sdkgo.TriggerTarget[MessageEvent], error) {
		inbox, err := project.TriggerInbox(key)
		if err != nil {
			return nil, err
		}
		return provider.NewDurableTriggerTarget(inbox, key, target, provider.WithTriggerLogger(connection.client.logger))
	})
}

// newDurableMessageTriggerRunner runs config's routes on connection with the bindings that configuration
// stores, wrapping each target with makeDurable.
func newDurableMessageTriggerRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	config ProjectMessageTriggerRunnerConfig,
	makeDurable durableMessageTriggerTarget,
) (*MessageTriggerRunner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	runnerConfig := MessageTriggerRunnerConfig{Connection: connection}
	for _, route := range config.ChannelThreadCreatedRoutes {
		var routeConfiguration ChannelThreadCreatedTriggerConfiguration
		target, err := newDurableMessageTriggerTarget(connection, configuration, makeDurable,
			ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName, route.BindingName, route.Target, &routeConfiguration)
		if err != nil {
			return nil, err
		}
		runnerConfig.ChannelThreadCreatedRoutes = append(runnerConfig.ChannelThreadCreatedRoutes, ChannelThreadCreatedTriggerRoute{
			BindingName: route.BindingName, Configuration: routeConfiguration, Target: target,
		})
	}
	for _, route := range config.ThreadReplyCreatedRoutes {
		var routeConfiguration ThreadReplyCreatedTriggerConfiguration
		target, err := newDurableMessageTriggerTarget(connection, configuration, makeDurable,
			ThreadReplyCreatedTriggerDefinition.Trigger.TriggerName, route.BindingName, route.Target, &routeConfiguration)
		if err != nil {
			return nil, err
		}
		runnerConfig.ThreadReplyCreatedRoutes = append(runnerConfig.ThreadReplyCreatedRoutes, ThreadReplyCreatedTriggerRoute{
			BindingName: route.BindingName, Configuration: routeConfiguration, Target: target,
		})
	}
	return NewMessageTriggerRunner(runnerConfig)
}

// newDurableMessageTriggerTarget decodes one binding's configuration into routeConfiguration and wraps target
// with makeDurable under the binding's inbox key.
func newDurableMessageTriggerTarget(
	connection Connection,
	configuration projectconfig.Configuration,
	makeDurable durableMessageTriggerTarget,
	triggerName string,
	bindingName string,
	target sdkgo.TriggerTarget[MessageEvent],
	routeConfiguration any,
) (sdkgo.TriggerTarget[MessageEvent], error) {
	connectionName := connection.reference.Name
	if err := configuration.DecodeTriggerConfiguration(ConnectorID, connectionName, triggerName, bindingName, routeConfiguration); err != nil {
		return nil, fmt.Errorf("slack %s binding %q configuration: %w", triggerName, bindingName, err)
	}
	return makeDurable(projectconfig.TriggerInboxKey{
		ConnectorID: ConnectorID, ConnectionName: connectionName, TriggerName: triggerName, BindingName: bindingName,
	}, target)
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
