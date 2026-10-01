// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// LocalInviteeEventReceivedTriggerRoute binds one stored inviteeEventReceived binding to its application target.
type LocalInviteeEventReceivedTriggerRoute struct {
	// BindingName names the stored Trigger binding whose configuration filters the events.
	BindingName string
	// Target receives the binding's events through its durable local inbox, such as a
	// sdkgo.NewDexFlowTriggerTarget that starts one Flow per event ID.
	Target sdkgo.TriggerTarget[InviteeEvent]
}

// InviteeEventReceivedEndpointRunner serves one connection's Calendly webhook endpoint while running its
// bindings. An application mounts it as an http.Handler at its subscription's callback URL and runs it for
// the life of the process. It is safe for concurrent requests.
type InviteeEventReceivedEndpointRunner struct {
	endpointRunner *webhooktrigger.EndpointRunner
	endpoint       *webhooktrigger.Endpoint[Credentials, InviteeEvent]
}

// NewLocalInviteeEventReceivedEndpointRunner loads the connection and every route's stored binding from
// store and wraps each target in a durable inbox, so an event is on disk before Calendly receives 200. A
// route whose binding is not stored or is invalid returns an error. WithLogger also applies to the inboxes.
func NewLocalInviteeEventReceivedEndpointRunner(
	store *localconfig.Store,
	connectionName string,
	routes []LocalInviteeEventReceivedTriggerRoute,
	options ...Option,
) (*InviteeEventReceivedEndpointRunner, error) {
	if store == nil {
		return nil, fmt.Errorf("local connector configuration store is required")
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("Calendly inviteeEventReceived routes are required")
	}
	connection, err := NewLocalConnection(store, connectionName, options...)
	if err != nil {
		return nil, err
	}
	endpoint, err := connection.client.inviteeEventReceivedWebhookEndpoint(connection.reference)
	if err != nil {
		return nil, err
	}
	triggerName := InviteeEventReceivedTriggerDefinition.Trigger.TriggerName
	runners := make([]sdkgo.TriggerRunner, 0, len(routes))
	bindingNames := make(map[string]bool, len(routes))
	for _, route := range routes {
		bindingName := strings.TrimSpace(route.BindingName)
		if bindingName == "" || route.Target == nil {
			return nil, fmt.Errorf("Calendly inviteeEventReceived binding name and target are required")
		}
		if bindingNames[bindingName] {
			return nil, fmt.Errorf("Calendly inviteeEventReceived binding name %q is duplicated", bindingName)
		}
		bindingNames[bindingName] = true
		var configuration InviteeEventReceivedTriggerConfiguration
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
		runners = append(runners, NewInviteeEventReceivedTrigger(InviteeEventReceivedTriggerConfig{
			Connection: connection, ConnectionName: connectionName, BindingName: bindingName,
			Configuration: configuration, Target: durableTarget,
		}))
	}
	endpointRunner, err := webhooktrigger.NewEndpointRunner(endpoint, runners...)
	if err != nil {
		return nil, err
	}
	return &InviteeEventReceivedEndpointRunner{endpointRunner: endpointRunner, endpoint: endpoint}, nil
}

// ServeHTTP verifies, records, and acknowledges one Calendly delivery. It answers 503 until Run has replayed
// every binding's pending events and started receiving, so Calendly retries.
func (runner *InviteeEventReceivedEndpointRunner) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	runner.endpointRunner.ServeHTTP(response, request)
}

// Run replays each binding's durable inbox and then delivers new events until ctx ends or a binding fails.
// It returns ctx's error after cancellation.
func (runner *InviteeEventReceivedEndpointRunner) Run(ctx context.Context) error {
	return runner.endpointRunner.Run(ctx)
}

// RunningSourceCount reports how many bindings are receiving events. A readiness check can wait until it
// equals the number of routes; until it is positive, every delivery is answered 503.
func (runner *InviteeEventReceivedEndpointRunner) RunningSourceCount() int {
	return runner.endpoint.RunningSourceCount()
}
