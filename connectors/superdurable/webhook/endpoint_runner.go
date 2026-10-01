// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// LocalRequestReceivedTriggerRoute binds one stored requestReceived binding to its application target.
type LocalRequestReceivedTriggerRoute struct {
	// BindingName names the stored Trigger binding whose configuration filters the events.
	BindingName string
	// Target receives the binding's events through its durable local inbox, such as a
	// sdkgo.NewDexFlowTriggerTarget that starts one Flow per event ID.
	Target sdkgo.TriggerTarget[WebhookRequestEvent]
}

// RequestReceivedEndpointRunner serves one connection's webhook endpoint while running its bindings. An
// application mounts it as an http.Handler at the URL configured in the sender and runs it for the life
// of the process. It is safe for concurrent requests.
type RequestReceivedEndpointRunner struct {
	endpointRunner *webhooktrigger.EndpointRunner
	endpoint       *webhooktrigger.Endpoint[Credentials, WebhookRequestEvent]
}

// NewLocalRequestReceivedEndpointRunner loads the connection and every route's stored binding from store
// and wraps each target in a durable inbox, so an event is on disk before the sender receives 200. A
// route whose binding is not stored or is invalid returns an error. WithLogger also applies to the inboxes.
func NewLocalRequestReceivedEndpointRunner(
	store *localconfig.Store,
	connectionName string,
	routes []LocalRequestReceivedTriggerRoute,
	options ...Option,
) (*RequestReceivedEndpointRunner, error) {
	if store == nil {
		return nil, fmt.Errorf("local connector configuration store is required")
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("webhook requestReceived routes are required")
	}
	connection, err := NewLocalConnection(store, connectionName, options...)
	if err != nil {
		return nil, err
	}
	endpoint, err := connection.client.requestReceivedWebhookEndpoint(connection.reference)
	if err != nil {
		return nil, err
	}
	triggerName := RequestReceivedTriggerDefinition.Trigger.TriggerName
	runners := make([]sdkgo.TriggerRunner, 0, len(routes))
	bindingNames := make(map[string]bool, len(routes))
	for _, route := range routes {
		bindingName := strings.TrimSpace(route.BindingName)
		if bindingName == "" || route.Target == nil {
			return nil, fmt.Errorf("webhook requestReceived binding name and target are required")
		}
		if bindingNames[bindingName] {
			return nil, fmt.Errorf("webhook requestReceived binding name %q is duplicated", bindingName)
		}
		bindingNames[bindingName] = true
		var configuration RequestReceivedTriggerConfiguration
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
		runners = append(runners, NewRequestReceivedTrigger(RequestReceivedTriggerConfig{
			Connection: connection, ConnectionName: connectionName, BindingName: bindingName,
			Configuration: configuration, Target: durableTarget,
		}))
	}
	endpointRunner, err := webhooktrigger.NewEndpointRunner(endpoint, runners...)
	if err != nil {
		return nil, err
	}
	return &RequestReceivedEndpointRunner{endpointRunner: endpointRunner, endpoint: endpoint}, nil
}

// ServeHTTP verifies, records, and acknowledges one webhook request. It answers 503 until Run has replayed
// every binding's pending events and started receiving, so the sender retries.
func (runner *RequestReceivedEndpointRunner) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	runner.endpointRunner.ServeHTTP(response, request)
}

// Run replays each binding's durable inbox and then delivers new events until ctx ends or a binding fails.
// It returns ctx's error after cancellation.
func (runner *RequestReceivedEndpointRunner) Run(ctx context.Context) error {
	return runner.endpointRunner.Run(ctx)
}

// RunningSourceCount reports how many bindings are receiving events. A readiness check can wait until it
// equals the number of routes; until it is positive, every request is answered 503.
func (runner *RequestReceivedEndpointRunner) RunningSourceCount() int {
	return runner.endpoint.RunningSourceCount()
}
