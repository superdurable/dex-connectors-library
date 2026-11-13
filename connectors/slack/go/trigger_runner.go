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

// ButtonClickedTriggerRoute binds one button click configuration to its application target.
type ButtonClickedTriggerRoute struct {
	// BindingName names this configured Trigger binding.
	BindingName string
	// Configuration is the configuration for button clicked trigger route.
	Configuration ButtonClickedTriggerConfiguration
	// Target routes accepted events into the application.
	Target sdkgo.TriggerTarget[ButtonClickEvent]
}

// MessageTriggerRunnerConfig configures Slack message and button click Triggers that share one Socket Mode
// connection. Slack sends each event to only one of an app's open Socket Mode connections, so give every
// Trigger of a connection to one runner instead of running a separate Trigger for each.
type MessageTriggerRunnerConfig struct {
	// Connection specifies connection for message trigger runner config.
	Connection Connection
	// ChannelThreadCreatedRoutes specifies channel thread created routes for message trigger runner config.
	ChannelThreadCreatedRoutes []ChannelThreadCreatedTriggerRoute
	// ThreadReplyCreatedRoutes specifies thread reply created routes for message trigger runner config.
	ThreadReplyCreatedRoutes []ThreadReplyCreatedTriggerRoute
	// ButtonClickedRoutes receives clicks on message buttons. Clicks arrive only through Socket Mode, so the
	// connection's credential needs an app_token and the Slack app must have Interactivity turned on. With
	// button routes, every route's Target must be a durable inbox from provider.NewDurableTriggerTarget, so a
	// click can be acknowledged within three seconds without risking its loss; NewMessageTriggerRunner returns
	// an error otherwise. NewProjectMessageTriggerRunner always supplies durable inboxes.
	ButtonClickedRoutes []ButtonClickedTriggerRoute
}

// MessageTriggerRunner receives Slack messages for one connection and dispatches matching routes. It receives
// them through Socket Mode when the connection's credential has an app_token. A runner from
// NewProjectMessageTriggerRunner whose shared connection has none receives the events that its host relays
// instead, through ServeHTTP.
type MessageTriggerRunner struct {
	routes socketModeRoutes
	// relay receives relayed events; it is nil for a runner built without a project configuration.
	relay *relayedMessageReceiver
}

// NewMessageTriggerRunner creates one runner for all configured Slack message Trigger routes.
func NewMessageTriggerRunner(config MessageTriggerRunnerConfig) (*MessageTriggerRunner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	routes := make([]messageTriggerRoute, 0, len(config.ChannelThreadCreatedRoutes)+len(config.ThreadReplyCreatedRoutes))
	bindingKeys := make(map[string]bool, cap(routes)+len(config.ButtonClickedRoutes))
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
	buttonRoutes := make([]buttonClickedTriggerRoute, 0, len(config.ButtonClickedRoutes))
	for _, route := range config.ButtonClickedRoutes {
		if err := validateTriggerRoute("buttonClicked", route.BindingName, route.Target == nil, bindingKeys); err != nil {
			return nil, err
		}
		if err := route.Configuration.Validate(); err != nil {
			return nil, err
		}
		source := config.Connection.client.buttonClickedTriggerSource(config.Connection.reference, route.Configuration)
		source.bindingName = route.BindingName
		buttonRoutes = append(buttonRoutes, buttonClickedTriggerRoute{source: source, target: route.Target})
	}
	if len(routes) == 0 && len(buttonRoutes) == 0 {
		return nil, fmt.Errorf("Slack message Trigger routes are required")
	}
	runnerRoutes := socketModeRoutes{messages: routes, buttons: buttonRoutes}
	if len(buttonRoutes) > 0 && !runnerRoutes.hasDurableInboxes() {
		return nil, errButtonClicksRequireDurableInboxes
	}
	return &MessageTriggerRunner{routes: runnerRoutes}, nil
}

// Run replays each route's durable inbox in order, roots first, then replies, then button clicks, retrying
// failures until the context ends, then receives and dispatches Slack events until the context ends or the
// runner fails. Before each reconnect it replays the inboxes again, so an event persisted on a failed
// connection is delivered before any envelope read on the next one.
//
// When every route's target is a durable inbox, as with NewProjectMessageTriggerRunner, Run acknowledges each
// Socket Mode envelope once the matching inboxes recorded it and delivers acknowledged events in arrival
// order on a separate goroutine. A target that is still retrying, such as one waiting for a stopped Worker,
// therefore does not delay the acknowledgement of a later button click, which Slack requires within three
// seconds. Otherwise Run delivers each event before reading the next envelope, so at most one acknowledged
// event awaits delivery and a retrying event delays later envelopes.
//
// A runner from NewProjectMessageTriggerRunner first reads the connection's credential, retrying with the
// Socket Mode backoff while it is unavailable. With an app_token it uses Socket Mode. Without one, on a shared
// connection, it replays the inboxes, serves relayed events through ServeHTTP and registers the connection
// name with the host, retrying while the host is unavailable; it returns an error when the host refuses the
// registration, when the scope no longer selects the connection, and for a connection that is not shared.
// A host relays only Events API messages, so a relayed runner with button click routes returns an error.
//
// Run logs through the connection's WithLogger logger: INFO when a Socket Mode connection opens, WARN with
// the next delay when one fails, DEBUG for every ignored message with its reason, and the sdkgo delivery
// records (skips, retries with their backoff delay, and recoveries) with the route's binding.
func (runner *MessageTriggerRunner) Run(ctx context.Context) error {
	if runner.relay != nil {
		isRelayed, err := runner.relay.selectRelayedMode(ctx)
		if err != nil {
			return err
		}
		if isRelayed && len(runner.routes.buttons) > 0 {
			return fmt.Errorf("Slack connection %q has no app_token for Socket Mode, and button clicks reach only Socket Mode; add an app_token or remove the buttonClicked routes", runner.relay.key.ConnectionName)
		}
		if isRelayed {
			return runner.relay.run(ctx)
		}
	}
	if err := runner.routes.replay(ctx); err != nil {
		return err
	}
	return runSocketModeRoutes(ctx, runner.routes)
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

// ProjectButtonClickedTriggerRoute binds one button click binding from the project configuration to its
// application target.
type ProjectButtonClickedTriggerRoute struct {
	// BindingName names this configured Trigger binding.
	BindingName string
	// Target routes accepted events into the application.
	Target sdkgo.TriggerTarget[ButtonClickEvent]
}

// ProjectMessageTriggerRunnerConfig selects Slack message and button click Trigger bindings from the project
// configuration.
type ProjectMessageTriggerRunnerConfig struct {
	// ChannelThreadCreatedRoutes specifies channel thread created routes for project message trigger runner config.
	ChannelThreadCreatedRoutes []ProjectChannelThreadCreatedTriggerRoute
	// ThreadReplyCreatedRoutes specifies thread reply created routes for project message trigger runner config.
	ThreadReplyCreatedRoutes []ProjectThreadReplyCreatedTriggerRoute
	// ButtonClickedRoutes receives clicks on message buttons through Socket Mode; see MessageTriggerRunnerConfig.
	ButtonClickedRoutes []ProjectButtonClickedTriggerRoute
}

// durableMessageTriggerTarget wraps one binding's target so that each event is stored before its
// acknowledgement.
type durableMessageTriggerTarget func(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[MessageEvent],
) (sdkgo.TriggerTarget[MessageEvent], error)

// NewProjectMessageTriggerRunner opens connectionName and its configured bindings from the loaded project
// configuration and creates one durable runner, which uses Socket Mode or the host's relay as Run describes.
// Mount the runner at RelayedTriggerPath(connectionName) on the application's HTTP server. Each route's target
// receives its events through the binding's durable project inbox, which every replica of the application
// shares. A route whose binding is not configured returns an error. The WithLogger option also applies to the durable inboxes it creates.
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
	runner, err := newDurableMessageTriggerRunner(connection, project.Configuration, config, func(
		key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[MessageEvent],
	) (sdkgo.TriggerTarget[MessageEvent], error) {
		inbox, err := project.TriggerInbox(key)
		if err != nil {
			return nil, err
		}
		return provider.NewDurableTriggerTarget(inbox, key, target, provider.WithTriggerLogger(connection.client.logger))
	}, func(
		key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[ButtonClickEvent],
	) (sdkgo.TriggerTarget[ButtonClickEvent], error) {
		inbox, err := project.TriggerInbox(key)
		if err != nil {
			return nil, err
		}
		return provider.NewDurableTriggerTarget(inbox, key, target, provider.WithTriggerLogger(connection.client.logger))
	})
	if err != nil {
		return nil, err
	}
	if runner.relay, err = newRelayedMessageReceiver(project, connection, runner.routes.identity(), runner.routes.messages); err != nil {
		return nil, err
	}
	return runner, nil
}

// durableButtonClickedTriggerTarget wraps one button binding's target so that each click is stored before
// its acknowledgement.
type durableButtonClickedTriggerTarget func(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[ButtonClickEvent],
) (sdkgo.TriggerTarget[ButtonClickEvent], error)

// newDurableMessageTriggerRunner runs config's routes on connection with the bindings that configuration
// stores, wrapping each message target with makeDurable and each button target with makeButtonDurable.
func newDurableMessageTriggerRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	config ProjectMessageTriggerRunnerConfig,
	makeDurable durableMessageTriggerTarget,
	makeButtonDurable durableButtonClickedTriggerTarget,
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
	for _, route := range config.ButtonClickedRoutes {
		var routeConfiguration ButtonClickedTriggerConfiguration
		triggerName := ButtonClickedTriggerDefinition.Trigger.TriggerName
		connectionName := connection.reference.Name
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connectionName, triggerName, route.BindingName, &routeConfiguration); err != nil {
			return nil, fmt.Errorf("slack %s binding %q configuration: %w", triggerName, route.BindingName, err)
		}
		target, err := makeButtonDurable(projectconfig.TriggerInboxKey{
			ConnectorID: ConnectorID, ConnectionName: connectionName, TriggerName: triggerName, BindingName: route.BindingName,
		}, route.Target)
		if err != nil {
			return nil, err
		}
		runnerConfig.ButtonClickedRoutes = append(runnerConfig.ButtonClickedRoutes, ButtonClickedTriggerRoute{
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
	return validateTriggerRoute(triggerName, bindingName, target == nil, bindingKeys)
}

func validateTriggerRoute(triggerName string, bindingName string, isTargetMissing bool, bindingKeys map[string]bool) error {
	trimmedBindingName := strings.TrimSpace(bindingName)
	if trimmedBindingName == "" || isTargetMissing {
		return fmt.Errorf("Slack message Trigger binding name and target are required")
	}
	bindingKey := triggerName + "\x00" + trimmedBindingName
	if bindingKeys[bindingKey] {
		return fmt.Errorf("Slack message Trigger binding name %q is duplicated", trimmedBindingName)
	}
	bindingKeys[bindingKey] = true
	return nil
}
