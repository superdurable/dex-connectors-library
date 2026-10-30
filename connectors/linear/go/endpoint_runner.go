// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

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

// ProjectIssueEventReceivedTriggerRoute binds one issueEventReceived binding from the project configuration
// to its application target.
type ProjectIssueEventReceivedTriggerRoute struct {
	// BindingName names the configured Trigger binding whose configuration filters the events.
	BindingName string
	// Target receives the binding's events through its durable project inbox, such as a
	// sdkgo.NewDexFlowTriggerTarget that starts one Flow per event ID.
	Target sdkgo.TriggerTarget[IssueEvent]
}

// IssueEventReceivedEndpointRunner serves one connection's Linear webhook endpoint while running its
// bindings. An application mounts it as an http.Handler at its webhook's URL and runs it for the life of
// the process. It is safe for concurrent requests.
type IssueEventReceivedEndpointRunner struct {
	endpointRunner *webhooktrigger.EndpointRunner
	endpoint       *webhooktrigger.Endpoint[Credentials, IssueEvent]
}

// durableIssueEventReceivedTarget wraps one binding's target so that each event is stored before its
// acknowledgement.
type durableIssueEventReceivedTarget func(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[IssueEvent],
) (sdkgo.TriggerTarget[IssueEvent], error)

// NewProjectIssueEventReceivedEndpointRunner opens connectionName and every route's binding from the loaded
// project configuration and wraps each target in the binding's durable project inbox, so an event is stored
// before Linear receives 200. A route whose binding is not configured or is invalid returns an error.
// WithLogger also applies to the inboxes.
func NewProjectIssueEventReceivedEndpointRunner(
	project *projectconfig.LoadedProject,
	connectionName string,
	routes []ProjectIssueEventReceivedTriggerRoute,
	options ...Option,
) (*IssueEventReceivedEndpointRunner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newIssueEventReceivedEndpointRunner(connection, project.Configuration, routes, func(
		key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[IssueEvent],
	) (sdkgo.TriggerTarget[IssueEvent], error) {
		inbox, err := project.TriggerInbox(key)
		if err != nil {
			return nil, err
		}
		return provider.NewDurableTriggerTarget(inbox, key, target, provider.WithTriggerLogger(connection.client.logger))
	})
}

// newIssueEventReceivedEndpointRunner runs routes on connection with the bindings that configuration stores,
// wrapping each target with makeDurable.
func newIssueEventReceivedEndpointRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	routes []ProjectIssueEventReceivedTriggerRoute,
	makeDurable durableIssueEventReceivedTarget,
) (*IssueEventReceivedEndpointRunner, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("Linear issueEventReceived routes are required")
	}
	endpoint, err := connection.client.issueEventReceivedWebhookEndpoint(connection.reference)
	if err != nil {
		return nil, err
	}
	connectionName := connection.reference.Name
	triggerName := IssueEventReceivedTriggerDefinition.Trigger.TriggerName
	runners := make([]sdkgo.TriggerRunner, 0, len(routes))
	bindingNames := make(map[string]bool, len(routes))
	for _, route := range routes {
		bindingName := strings.TrimSpace(route.BindingName)
		if bindingName == "" || route.Target == nil {
			return nil, fmt.Errorf("Linear issueEventReceived binding name and target are required")
		}
		if bindingNames[bindingName] {
			return nil, fmt.Errorf("Linear issueEventReceived binding name %q is duplicated", bindingName)
		}
		bindingNames[bindingName] = true
		var bindingConfiguration IssueEventReceivedTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connectionName, triggerName, bindingName, &bindingConfiguration); err != nil {
			return nil, fmt.Errorf("Linear issueEventReceived binding %q configuration: %w", bindingName, err)
		}
		if err := bindingConfiguration.Validate(); err != nil {
			return nil, fmt.Errorf("binding %q: %w", bindingName, err)
		}
		key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: connectionName, TriggerName: triggerName, BindingName: bindingName}
		durableTarget, err := makeDurable(key, route.Target)
		if err != nil {
			return nil, err
		}
		runners = append(runners, NewIssueEventReceivedTrigger(IssueEventReceivedTriggerConfig{
			Connection: connection, ConnectionName: connectionName, BindingName: bindingName,
			Configuration: bindingConfiguration, Target: durableTarget,
		}))
	}
	endpointRunner, err := webhooktrigger.NewEndpointRunner(endpoint, runners...)
	if err != nil {
		return nil, err
	}
	return &IssueEventReceivedEndpointRunner{endpointRunner: endpointRunner, endpoint: endpoint}, nil
}

// ServeHTTP verifies, records, and acknowledges one Linear delivery. It answers 503 until Run has replayed
// every binding's pending events and started receiving, so Linear retries.
func (runner *IssueEventReceivedEndpointRunner) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	runner.endpointRunner.ServeHTTP(response, request)
}

// Run replays each binding's durable inbox and then delivers new events until ctx ends or a binding fails.
// It returns ctx's error after cancellation.
func (runner *IssueEventReceivedEndpointRunner) Run(ctx context.Context) error {
	return runner.endpointRunner.Run(ctx)
}

// RunningSourceCount reports how many bindings are receiving events. A readiness check can wait until it
// equals the number of routes; until it is positive, every delivery is answered 503.
func (runner *IssueEventReceivedEndpointRunner) RunningSourceCount() int {
	return runner.endpoint.RunningSourceCount()
}
