// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// MessageReceivedTriggerRoute binds one root-message configuration to its application target.
type MessageReceivedTriggerRoute struct {
	BindingName   string
	Configuration MessageReceivedTriggerConfiguration
	Target        sdkgo.TriggerTarget[MessageEvent]
}

// ReplyReceivedTriggerRoute binds one reply configuration to its application target.
type ReplyReceivedTriggerRoute struct {
	BindingName   string
	Configuration ReplyReceivedTriggerConfiguration
	Target        sdkgo.TriggerTarget[MessageEvent]
}

// MessageTriggerRunnerConfig configures Gmail message Triggers that share one ordered poller.
type MessageTriggerRunnerConfig struct {
	Connection            Connection
	MessageReceivedRoutes []MessageReceivedTriggerRoute
	ReplyReceivedRoutes   []ReplyReceivedTriggerRoute
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

// LocalMessageReceivedTriggerRoute binds one stored root-message configuration to its application target.
type LocalMessageReceivedTriggerRoute struct {
	BindingName string
	Target      sdkgo.TriggerTarget[MessageEvent]
}

// LocalReplyReceivedTriggerRoute binds one stored reply configuration to its application target.
type LocalReplyReceivedTriggerRoute struct {
	BindingName string
	Target      sdkgo.TriggerTarget[MessageEvent]
}

// LocalMessageTriggerRunnerConfig selects stored Gmail message Trigger bindings.
type LocalMessageTriggerRunnerConfig struct {
	MessageReceivedRoutes []LocalMessageReceivedTriggerRoute
	ReplyReceivedRoutes   []LocalReplyReceivedTriggerRoute
}

// NewLocalMessageTriggerRunner loads stored bindings and creates one durable, ordered Gmail runner. The
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
	for _, route := range config.MessageReceivedRoutes {
		var configuration MessageReceivedTriggerConfiguration
		if err := store.DecodeTriggerConfiguration(ConnectorID, connectionName, "messageReceived", route.BindingName, &configuration); err != nil {
			return nil, err
		}
		target, err := localconfig.NewDurableTriggerTarget(store, ConnectorID, connectionName, "messageReceived", route.BindingName, route.Target,
			localconfig.WithTriggerLogger(connection.client.logger))
		if err != nil {
			return nil, err
		}
		runnerConfig.MessageReceivedRoutes = append(runnerConfig.MessageReceivedRoutes, MessageReceivedTriggerRoute{
			BindingName: route.BindingName, Configuration: configuration, Target: target,
		})
	}
	for _, route := range config.ReplyReceivedRoutes {
		var configuration ReplyReceivedTriggerConfiguration
		if err := store.DecodeTriggerConfiguration(ConnectorID, connectionName, "replyReceived", route.BindingName, &configuration); err != nil {
			return nil, err
		}
		target, err := localconfig.NewDurableTriggerTarget(store, ConnectorID, connectionName, "replyReceived", route.BindingName, route.Target,
			localconfig.WithTriggerLogger(connection.client.logger))
		if err != nil {
			return nil, err
		}
		runnerConfig.ReplyReceivedRoutes = append(runnerConfig.ReplyReceivedRoutes, ReplyReceivedTriggerRoute{
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
		return fmt.Errorf("Gmail message Trigger binding name and target are required")
	}
	bindingKey := triggerName + "\x00" + trimmedBindingName
	if bindingKeys[bindingKey] {
		return fmt.Errorf("Gmail message Trigger binding name %q is duplicated", trimmedBindingName)
	}
	bindingKeys[bindingKey] = true
	return nil
}
