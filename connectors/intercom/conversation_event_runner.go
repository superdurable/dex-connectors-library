// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// ProjectConversationEventTriggerRoute binds one stored conversationEvent binding to its application target.
type ProjectConversationEventTriggerRoute struct {
	// BindingName names the stored Trigger binding whose configuration selects the topics.
	BindingName string
	// Target receives the binding's events through its durable project inbox, such as a
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

// durableConversationEventTarget wraps one binding's target so that each event is stored before its acknowledgement.
type durableConversationEventTarget func(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[ConversationEvent],
) (sdkgo.TriggerTarget[ConversationEvent], error)

// NewProjectConversationEventEndpointRunner loads the connection and every route's stored binding from
// project and wraps each target in a durable inbox, so a notification is in project storage before Intercom
// receives 200. A route whose binding is not stored or is invalid returns an error. WithLogger also applies
// to the inboxes.
func NewProjectConversationEventEndpointRunner(
	project *projectconfig.LoadedProject,
	connectionName string,
	routes []ProjectConversationEventTriggerRoute,
	options ...Option,
) (*ConversationEventEndpointRunner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newConversationEventEndpointRunner(connection, project.Configuration, routes, func(
		key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[ConversationEvent],
	) (sdkgo.TriggerTarget[ConversationEvent], error) {
		inbox, err := project.TriggerInbox(key)
		if err != nil {
			return nil, err
		}
		return provider.NewDurableTriggerTarget(inbox, key, target, provider.WithTriggerLogger(connection.client.logger))
	})
}

// newConversationEventEndpointRunner runs routes on connection with the bindings that configuration stores.
func newConversationEventEndpointRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	routes []ProjectConversationEventTriggerRoute,
	makeDurable durableConversationEventTarget,
) (*ConversationEventEndpointRunner, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("Intercom conversationEvent routes are required")
	}
	endpoint, err := connection.client.conversationEventWebhookEndpoint(connection.reference)
	if err != nil {
		return nil, err
	}
	connectionName := connection.reference.Name
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
		var bindingConfiguration ConversationEventTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connectionName, triggerName, bindingName, &bindingConfiguration); err != nil {
			return nil, fmt.Errorf("Intercom conversationEvent binding %q configuration: %w", bindingName, err)
		}
		if err := bindingConfiguration.Validate(); err != nil {
			return nil, fmt.Errorf("binding %q: %w", bindingName, err)
		}
		key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: connectionName, TriggerName: triggerName, BindingName: bindingName}
		durableTarget, err := makeDurable(key, route.Target)
		if err != nil {
			return nil, err
		}
		runners = append(runners, NewConversationEventTrigger(ConversationEventTriggerConfig{
			Connection: connection, ConnectionName: connectionName, BindingName: bindingName,
			Configuration: bindingConfiguration, Target: durableTarget,
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
