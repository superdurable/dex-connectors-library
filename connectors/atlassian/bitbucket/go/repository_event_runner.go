// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

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

// ProjectRepositoryEventReceivedTriggerRoute binds a configured Trigger to an application target.
type ProjectRepositoryEventReceivedTriggerRoute struct {
	// BindingName selects the configured repositoryEventReceived binding.
	BindingName string
	// Target receives events after the durable project inbox records them.
	Target sdkgo.TriggerTarget[RepositoryEvent]
}

// RepositoryEventReceivedEndpointRunner serves signed webhooks and durable binding routes.
type RepositoryEventReceivedEndpointRunner struct {
	endpointRunner *webhooktrigger.EndpointRunner
	endpoint       *webhooktrigger.Endpoint[Credentials, RepositoryEvent]
}

type durableRepositoryEventTarget func(projectconfig.TriggerInboxKey, sdkgo.TriggerTarget[RepositoryEvent]) (sdkgo.TriggerTarget[RepositoryEvent], error)

// NewProjectRepositoryEventReceivedEndpointRunner opens one connection and the configured binding routes.
// Each route is wrapped in a durable inbox before Bitbucket receives an acknowledgement.
func NewProjectRepositoryEventReceivedEndpointRunner(project *projectconfig.LoadedProject, connectionName string, routes []ProjectRepositoryEventReceivedTriggerRoute, options ...Option) (*RepositoryEventReceivedEndpointRunner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newRepositoryEventReceivedEndpointRunner(connection, project.Configuration, routes, func(key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[RepositoryEvent]) (sdkgo.TriggerTarget[RepositoryEvent], error) {
		inbox, err := project.TriggerInbox(key)
		if err != nil {
			return nil, err
		}
		return provider.NewDurableTriggerTarget(inbox, key, target)
	})
}

func newRepositoryEventReceivedEndpointRunner(connection Connection, configuration projectconfig.Configuration, routes []ProjectRepositoryEventReceivedTriggerRoute, makeDurable durableRepositoryEventTarget) (*RepositoryEventReceivedEndpointRunner, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("Bitbucket repository event routes are required")
	}
	endpoint, err := connection.client.repositoryEventReceivedWebhookEndpoint(connection.reference)
	if err != nil {
		return nil, err
	}
	connectionName := connection.reference.Name
	triggerName := RepositoryEventReceivedTriggerDefinition.Trigger.TriggerName
	runners := make([]sdkgo.TriggerRunner, 0, len(routes))
	bindingNames := make(map[string]bool, len(routes))
	for _, route := range routes {
		bindingName := strings.TrimSpace(route.BindingName)
		if bindingName == "" || route.Target == nil {
			return nil, fmt.Errorf("Bitbucket repository event binding name and target are required")
		}
		if bindingNames[bindingName] {
			return nil, fmt.Errorf("Bitbucket repository event binding name %q is duplicated", bindingName)
		}
		bindingNames[bindingName] = true
		var bindingConfiguration RepositoryEventReceivedTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connectionName, triggerName, bindingName, &bindingConfiguration); err != nil {
			return nil, fmt.Errorf("Bitbucket binding %q configuration: %w", bindingName, err)
		}
		if err := bindingConfiguration.Validate(); err != nil {
			return nil, fmt.Errorf("Bitbucket binding %q: %w", bindingName, err)
		}
		key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: connectionName, TriggerName: triggerName, BindingName: bindingName}
		durableTarget, err := makeDurable(key, route.Target)
		if err != nil {
			return nil, err
		}
		runners = append(runners, NewRepositoryEventReceivedTrigger(RepositoryEventReceivedTriggerConfig{
			Connection: connection, ConnectionName: connectionName, BindingName: bindingName,
			Configuration: bindingConfiguration, Target: durableTarget,
		}))
	}
	endpointRunner, err := webhooktrigger.NewEndpointRunner(endpoint, runners...)
	if err != nil {
		return nil, err
	}
	return &RepositoryEventReceivedEndpointRunner{endpointRunner: endpointRunner, endpoint: endpoint}, nil
}

// ServeHTTP verifies, records, and acknowledges a Bitbucket webhook delivery.
func (runner *RepositoryEventReceivedEndpointRunner) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	runner.endpointRunner.ServeHTTP(response, request)
}

// Run replays durable inboxes and then delivers new events until cancellation.
func (runner *RepositoryEventReceivedEndpointRunner) Run(ctx context.Context) error {
	return runner.endpointRunner.Run(ctx)
}

// RunningSourceCount reports how many configured bindings are receiving events.
func (runner *RepositoryEventReceivedEndpointRunner) RunningSourceCount() int {
	return runner.endpoint.RunningSourceCount()
}
