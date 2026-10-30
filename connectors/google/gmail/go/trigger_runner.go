// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// MessageReceivedTriggerRoute binds one root-message configuration to its application target.
type MessageReceivedTriggerRoute struct {
	// BindingName names this configured Trigger binding.
	BindingName string
	// Configuration is the configuration for message received trigger route.
	Configuration MessageReceivedTriggerConfiguration
	// Target routes accepted events into the application.
	Target sdkgo.TriggerTarget[MessageEvent]
}

// ReplyReceivedTriggerRoute binds one reply configuration to its application target.
type ReplyReceivedTriggerRoute struct {
	// BindingName names this configured Trigger binding.
	BindingName string
	// Configuration is the configuration for reply received trigger route.
	Configuration ReplyReceivedTriggerConfiguration
	// Target routes accepted events into the application.
	Target sdkgo.TriggerTarget[MessageEvent]
}

// MessageTriggerRunnerConfig configures Gmail message Triggers that share one ordered poller.
type MessageTriggerRunnerConfig struct {
	// Connection specifies connection for message trigger runner config.
	Connection Connection
	// MessageReceivedRoutes specifies message received routes for message trigger runner config.
	MessageReceivedRoutes []MessageReceivedTriggerRoute
	// ReplyReceivedRoutes specifies reply received routes for message trigger runner config.
	ReplyReceivedRoutes []ReplyReceivedTriggerRoute
}

// MessageTriggerRunner polls every configured Gmail route and delivers root messages before replies.
type MessageTriggerRunner struct {
	rootRoutes   []messageTriggerRoute
	replyRoutes  []messageTriggerRoute
	pollInterval time.Duration
}

type messageTriggerRoute struct {
	source *messagePollingTriggerSource
	target sdkgo.TriggerTarget[MessageEvent]
}

// NewMessageTriggerRunner creates one ordered runner for all configured Gmail message Trigger routes.
func NewMessageTriggerRunner(config MessageTriggerRunnerConfig) (*MessageTriggerRunner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	runner := &MessageTriggerRunner{pollInterval: config.Connection.client.pollInterval}
	bindingKeys := make(map[string]bool, len(config.MessageReceivedRoutes)+len(config.ReplyReceivedRoutes))
	for _, route := range config.MessageReceivedRoutes {
		if err := validateMessageTriggerRoute("messageReceived", route.BindingName, route.Target, bindingKeys); err != nil {
			return nil, err
		}
		if err := route.Configuration.Validate(); err != nil {
			return nil, err
		}
		source := config.Connection.client.messageReceivedPollingSource(config.Connection.reference, route.Configuration)
		source.bindingName = route.BindingName
		runner.rootRoutes = append(runner.rootRoutes, messageTriggerRoute{source: source, target: route.Target})
	}
	for _, route := range config.ReplyReceivedRoutes {
		if err := validateMessageTriggerRoute("replyReceived", route.BindingName, route.Target, bindingKeys); err != nil {
			return nil, err
		}
		if err := route.Configuration.Validate(); err != nil {
			return nil, err
		}
		source := config.Connection.client.replyReceivedPollingSource(config.Connection.reference, route.Configuration)
		source.bindingName = route.BindingName
		runner.replyRoutes = append(runner.replyRoutes, messageTriggerRoute{source: source, target: route.Target})
	}
	if len(runner.rootRoutes)+len(runner.replyRoutes) == 0 {
		return nil, fmt.Errorf("Gmail message Trigger routes are required")
	}
	return runner, nil
}

// Run replays each route's durable inbox in order, roots first, retrying failures until the context ends.
// It then polls until the context ends. Each poll lists every reply route before it lists any root route,
// then delivers every root before any reply, and ends at the first failure. Every later poll retries the
// failed event before that route's listed messages, even after Gmail stops listing it. Gmail receives a
// thread's root before any reply to it, so the root of every listed reply is in the later root listing
// and reaches Dex first.
//
// Run logs through the connection's WithLogger logger: a WARN "gmail poll failed; retrying" record with the
// consecutive failed polls as its attempt and the poll interval as its delay when a list or read fails, a
// WARN "trigger delivery failed; retrying" record with the attempt number when a target fails, an INFO
// "trigger delivered after retry" record when a later poll delivers that message, DEBUG records for
// ignored messages, and the durable inbox records.
func (runner *MessageTriggerRunner) Run(ctx context.Context) error {
	for _, routes := range [][]messageTriggerRoute{runner.rootRoutes, runner.replyRoutes} {
		for _, route := range routes {
			if replayer, ok := route.target.(sdkgo.TriggerDeliveryReplayer); ok {
				if err := replayer.ReplayTriggerDeliveries(ctx); err != nil {
					return fmt.Errorf("replay Gmail Trigger deliveries: %w", err)
				}
			}
		}
	}
	for {
		if err := runner.poll(ctx); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		timer := time.NewTimer(runner.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// poll makes one ordered pass over every route. It lists replies before roots: a reply in the reply
// listing arrived after its root, so the root listing that follows contains that root too, unless an
// earlier poll delivered it. A reply that arrives after the reply listing waits for the next poll.
func (runner *MessageTriggerRunner) poll(ctx context.Context) error {
	replyListings := make([]messageListing, len(runner.replyRoutes))
	for index, route := range runner.replyRoutes {
		listing, err := route.source.list(ctx)
		if err != nil {
			return err
		}
		replyListings[index] = listing
	}
	for _, route := range runner.rootRoutes {
		if err := route.source.scan(ctx, route.target); err != nil {
			return err
		}
	}
	for index, route := range runner.replyRoutes {
		if err := route.source.deliver(ctx, route.target, replyListings[index]); err != nil {
			return err
		}
	}
	return nil
}

// ProjectMessageReceivedTriggerRoute binds one root-message binding saved in the project configuration to
// its application target.
type ProjectMessageReceivedTriggerRoute struct {
	// BindingName names this configured Trigger binding.
	BindingName string
	// Target routes accepted events into the application.
	Target sdkgo.TriggerTarget[MessageEvent]
}

// ProjectReplyReceivedTriggerRoute binds one reply binding saved in the project configuration to its
// application target.
type ProjectReplyReceivedTriggerRoute struct {
	// BindingName names this configured Trigger binding.
	BindingName string
	// Target routes accepted events into the application.
	Target sdkgo.TriggerTarget[MessageEvent]
}

// ProjectMessageTriggerRunnerConfig selects Gmail message Trigger bindings saved in the project configuration.
type ProjectMessageTriggerRunnerConfig struct {
	// MessageReceivedRoutes specifies message received routes for project message trigger runner config.
	MessageReceivedRoutes []ProjectMessageReceivedTriggerRoute
	// ReplyReceivedRoutes specifies reply received routes for project message trigger runner config.
	ReplyReceivedRoutes []ProjectReplyReceivedTriggerRoute
}

// NewProjectMessageTriggerRunner opens connectionName from the loaded project configuration, loads the
// selected bindings' saved configuration, and creates one durable, ordered Gmail runner. Each route keeps
// acknowledged events in its binding's project Trigger inbox until its target consumes them, so a
// restarted or replaced replica replays them. The WithLogger option also applies to those inboxes.
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
	return newDurableMessageTriggerRunner(connection, project.Configuration, project.TriggerInbox, config)
}

// newDurableMessageTriggerRunner wraps each route's target in the durable inbox that openInbox opens for its binding.
func newDurableMessageTriggerRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	openInbox func(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error),
	config ProjectMessageTriggerRunnerConfig,
) (*MessageTriggerRunner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	connectionName := connection.reference.Name
	runnerConfig := MessageTriggerRunnerConfig{Connection: connection}
	for _, route := range config.MessageReceivedRoutes {
		var bindingConfiguration MessageReceivedTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connectionName, "messageReceived", route.BindingName, &bindingConfiguration); err != nil {
			return nil, fmt.Errorf("gmail messageReceived binding %q configuration: %w", route.BindingName, err)
		}
		target, err := newDurableMessageTriggerTarget(connection, openInbox, "messageReceived", route.BindingName, route.Target)
		if err != nil {
			return nil, err
		}
		runnerConfig.MessageReceivedRoutes = append(runnerConfig.MessageReceivedRoutes, MessageReceivedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target,
		})
	}
	for _, route := range config.ReplyReceivedRoutes {
		var bindingConfiguration ReplyReceivedTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connectionName, "replyReceived", route.BindingName, &bindingConfiguration); err != nil {
			return nil, fmt.Errorf("gmail replyReceived binding %q configuration: %w", route.BindingName, err)
		}
		target, err := newDurableMessageTriggerTarget(connection, openInbox, "replyReceived", route.BindingName, route.Target)
		if err != nil {
			return nil, err
		}
		runnerConfig.ReplyReceivedRoutes = append(runnerConfig.ReplyReceivedRoutes, ReplyReceivedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target,
		})
	}
	return NewMessageTriggerRunner(runnerConfig)
}

// newDurableMessageTriggerTarget logs the inbox's records through the connection's WithLogger logger.
func newDurableMessageTriggerTarget(
	connection Connection,
	openInbox func(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error),
	triggerName string,
	bindingName string,
	target sdkgo.TriggerTarget[MessageEvent],
) (sdkgo.TriggerTarget[MessageEvent], error) {
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: connection.reference.Name, TriggerName: triggerName, BindingName: bindingName}
	inbox, err := openInbox(key)
	if err != nil {
		return nil, err
	}
	return provider.NewDurableTriggerTarget(inbox, key, target, provider.WithTriggerLogger(connection.client.logger))
}

func validateMessageTriggerRoute(
	triggerName string,
	bindingName string,
	target sdkgo.TriggerTarget[MessageEvent],
	bindingKeys map[string]bool,
) error {
	trimmedBindingName := strings.TrimSpace(bindingName)
	if trimmedBindingName == "" || target == nil {
		return fmt.Errorf("Gmail message Trigger binding name and target are required")
	}
	bindingKey := triggerName + "\x00" + trimmedBindingName
	if bindingKeys[bindingKey] {
		return fmt.Errorf("Gmail message Trigger binding name %q is duplicated", trimmedBindingName)
	}
	bindingKeys[bindingKey] = true
	return nil
}
