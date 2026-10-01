// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// LocalConversationEventTriggerRoute binds one stored conversationEvent binding to its application target.
type LocalConversationEventTriggerRoute struct {
	// BindingName names the stored Trigger binding whose configuration selects the topics.
	BindingName string
	// Target receives the binding's events through its durable local inbox, such as a
	// sdkgo.NewDexFlowTriggerTarget that starts one Flow per conversation.
	Target sdkgo.TriggerTarget[ConversationEvent]
}

// ConversationEventEndpointRunner serves one connection's Intercom webhook endpoint while running its
// bindings. An application mounts it as an http.Handler at the URL entered in the Intercom app's
// Configure > Webhooks page and runs it for the life of the process. It is safe for concurrent requests.
type ConversationEventEndpointRunner struct {
	endpointRunner *webhooktrigger.EndpointRunner
	endpoint       *webhooktrigger.Endpoint[Credentials, ConversationEvent]
	handler        http.Handler
}

// NewLocalConversationEventEndpointRunner loads the connection and every route's stored binding from
// store and wraps each target in a durable inbox, so a notification is on disk before Intercom receives
// 200. A route whose binding is not stored or is invalid returns an error. WithLogger also applies to
// the inboxes.
func NewLocalConversationEventEndpointRunner(
	store *localconfig.Store,
	connectionName string,
	routes []LocalConversationEventTriggerRoute,
	options ...Option,
) (*ConversationEventEndpointRunner, error) {
	if store == nil {
		return nil, fmt.Errorf("local connector configuration store is required")
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("Intercom conversationEvent routes are required")
	}
	connection, err := NewLocalConnection(store, connectionName, options...)
	if err != nil {
		return nil, err
	}
	endpoint, err := connection.client.conversationEventWebhookEndpoint(connection.reference)
	if err != nil {
		return nil, err
	}
	triggerName := ConversationEventTriggerDefinition.Trigger.TriggerName
	runners := make([]sdkgo.TriggerRunner, 0, len(routes))
	bindingNames := make(map[string]bool, len(routes))
	for _, route := range routes {
		bindingName := strings.TrimSpace(route.BindingName)
		if bindingName == "" || route.Target == nil {
			return nil, fmt.Errorf("Intercom conversationEvent binding name and target are required")
		}
		if bindingNames[bindingName] {
			return nil, fmt.Errorf("Intercom conversationEvent binding name %q is duplicated", bindingName)
		}
		bindingNames[bindingName] = true
		var configuration ConversationEventTriggerConfiguration
		if err := store.DecodeTriggerConfiguration(ConnectorID, connectionName, triggerName, bindingName, &configuration); err != nil {
			return nil, err
		}
		if err := configuration.Validate(); err != nil {
			return nil, fmt.Errorf("binding %q: %w", bindingName, err)
		}
		durableTarget, err := localconfig.NewDurableTriggerTarget(store, ConnectorID, connectionName, triggerName, bindingName,
			route.Target, localconfig.WithTriggerLogger(connection.client.logger))
		if err != nil {
			return nil, err
		}
		runners = append(runners, NewConversationEventTrigger(ConversationEventTriggerConfig{
			Connection: connection, ConnectionName: connectionName, BindingName: bindingName,
			Configuration: configuration, Target: durableTarget,
		}))
	}
	endpointRunner, err := webhooktrigger.NewEndpointRunner(endpoint, runners...)
	if err != nil {
		return nil, err
	}
	return &ConversationEventEndpointRunner{
		endpointRunner: endpointRunner, endpoint: endpoint, handler: headValidatingHandler{next: endpointRunner},
	}, nil
}

// ServeHTTP answers Intercom's HEAD validation request, then verifies, records, and acknowledges one
// notification. It answers 503 to a notification until Run has replayed every binding's pending events
// and started receiving, so Intercom retries.
func (runner *ConversationEventEndpointRunner) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	runner.handler.ServeHTTP(response, request)
}

// Run replays each binding's durable inbox and then delivers new notifications until ctx ends or a
// binding fails. It returns ctx's error after cancellation.
func (runner *ConversationEventEndpointRunner) Run(ctx context.Context) error {
	return runner.endpointRunner.Run(ctx)
}

// RunningSourceCount reports how many bindings are receiving notifications. A readiness check can wait
// until it equals the number of routes; until it is positive, every notification is answered 503.
func (runner *ConversationEventEndpointRunner) RunningSourceCount() int {
	return runner.endpoint.RunningSourceCount()
}
