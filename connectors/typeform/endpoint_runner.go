// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform

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

// ProjectResponseSubmittedTriggerRoute binds one stored responseSubmitted binding to its application target.
type ProjectResponseSubmittedTriggerRoute struct {
	// BindingName names the stored Trigger binding whose configuration filters the submissions.
	BindingName string
	// Target receives the binding's events through its durable project inbox, such as a
	// sdkgo.NewDexFlowTriggerTarget that starts one Flow per event ID.
	Target sdkgo.TriggerTarget[FormResponseEvent]
}

// ResponseSubmittedEndpointRunner serves one connection's Typeform webhook endpoint while running its
// bindings. An application mounts it as an http.Handler at its webhooks' URL and runs it for the life of
// the process. It is safe for concurrent requests.
type ResponseSubmittedEndpointRunner struct {
	endpointRunner *webhooktrigger.EndpointRunner
	endpoint       *webhooktrigger.Endpoint[Credentials, FormResponseEvent]
}

// durableResponseSubmittedTarget wraps one binding's target so that each event is stored before its acknowledgement.
type durableResponseSubmittedTarget func(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[FormResponseEvent],
) (sdkgo.TriggerTarget[FormResponseEvent], error)

// NewProjectResponseSubmittedEndpointRunner loads the connection and every route's stored binding from
// project and wraps each target in a durable inbox, so a submission is in project storage before Typeform
// receives 200. A route whose binding is not stored or is invalid returns an error. WithLogger also applies
// to the inboxes.
func NewProjectResponseSubmittedEndpointRunner(
	project *projectconfig.LoadedProject,
	connectionName string,
	routes []ProjectResponseSubmittedTriggerRoute,
	options ...Option,
) (*ResponseSubmittedEndpointRunner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newResponseSubmittedEndpointRunner(connection, project.Configuration, routes, func(
		key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[FormResponseEvent],
	) (sdkgo.TriggerTarget[FormResponseEvent], error) {
		inbox, err := project.TriggerInbox(key)
		if err != nil {
			return nil, err
		}
		return provider.NewDurableTriggerTarget(inbox, key, target, provider.WithTriggerLogger(connection.client.logger))
	})
}

// newResponseSubmittedEndpointRunner runs routes on connection with the bindings that configuration stores.
func newResponseSubmittedEndpointRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	routes []ProjectResponseSubmittedTriggerRoute,
	makeDurable durableResponseSubmittedTarget,
) (*ResponseSubmittedEndpointRunner, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("Typeform responseSubmitted routes are required")
	}
	endpoint, err := connection.client.responseSubmittedWebhookEndpoint(connection.reference)
	if err != nil {
		return nil, err
	}
	connectionName := connection.reference.Name
	triggerName := ResponseSubmittedTriggerDefinition.Trigger.TriggerName
	runners := make([]sdkgo.TriggerRunner, 0, len(routes))
	bindingNames := make(map[string]bool, len(routes))
	for _, route := range routes {
		bindingName := strings.TrimSpace(route.BindingName)
		if bindingName == "" || route.Target == nil {
			return nil, fmt.Errorf("Typeform responseSubmitted binding name and target are required")
		}
		if bindingNames[bindingName] {
			return nil, fmt.Errorf("Typeform responseSubmitted binding name %q is duplicated", bindingName)
		}
		bindingNames[bindingName] = true
		var bindingConfiguration ResponseSubmittedTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connectionName, triggerName, bindingName, &bindingConfiguration); err != nil {
			return nil, fmt.Errorf("Typeform responseSubmitted binding %q configuration: %w", bindingName, err)
		}
		if err := bindingConfiguration.Validate(); err != nil {
			return nil, fmt.Errorf("binding %q: %w", bindingName, err)
		}
		key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: connectionName, TriggerName: triggerName, BindingName: bindingName}
		durableTarget, err := makeDurable(key, route.Target)
		if err != nil {
			return nil, err
		}
		runners = append(runners, NewResponseSubmittedTrigger(ResponseSubmittedTriggerConfig{
			Connection: connection, ConnectionName: connectionName, BindingName: bindingName,
			Configuration: bindingConfiguration, Target: durableTarget,
		}))
	}
	endpointRunner, err := webhooktrigger.NewEndpointRunner(endpoint, runners...)
	if err != nil {
		return nil, err
	}
	return &ResponseSubmittedEndpointRunner{endpointRunner: endpointRunner, endpoint: endpoint}, nil
}

// ServeHTTP verifies, records, and acknowledges one Typeform delivery. It answers 503 until Run has
// replayed every binding's pending events and started receiving, so Typeform retries.
func (runner *ResponseSubmittedEndpointRunner) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	runner.endpointRunner.ServeHTTP(response, request)
}

// Run replays each binding's durable inbox and then delivers new events until ctx ends or a binding
// fails. It returns ctx's error after cancellation.
func (runner *ResponseSubmittedEndpointRunner) Run(ctx context.Context) error {
	return runner.endpointRunner.Run(ctx)
}

// RunningSourceCount reports how many bindings are receiving events. A readiness check can wait until it
// equals the number of routes; until it is positive, every delivery is answered 503.
func (runner *ResponseSubmittedEndpointRunner) RunningSourceCount() int {
	return runner.endpoint.RunningSourceCount()
}
