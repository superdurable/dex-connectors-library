// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup

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

// ProjectTaskEventTriggerRoute binds one stored taskEvent binding to its application target.
type ProjectTaskEventTriggerRoute struct {
	// BindingName names the stored Trigger binding whose configuration selects the events.
	BindingName string
	// Target receives the binding's events through its durable project inbox, such as a
	// sdkgo.NewDexFlowTriggerTarget that starts one Flow per status change.
	Target sdkgo.TriggerTarget[TaskEvent]
}

// TaskEventEndpointRunner serves one connection's ClickUp webhook endpoint while running its bindings.
// An application mounts it as an http.Handler at the endpoint registered with ClickUp's Create Webhook
// request and runs it for the life of the process. It is safe for concurrent requests.
type TaskEventEndpointRunner struct {
	endpointRunner *webhooktrigger.EndpointRunner
	endpoint       *webhooktrigger.Endpoint[Credentials, TaskEvent]
}

// durableTaskEventTarget wraps one binding's target so that each event is stored before its acknowledgement.
type durableTaskEventTarget func(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[TaskEvent],
) (sdkgo.TriggerTarget[TaskEvent], error)

// NewProjectTaskEventEndpointRunner loads the connection and every route's stored binding from project
// and wraps each target in a durable inbox, so an event is in project storage before ClickUp receives
// 200. A route whose binding is not stored or is invalid returns an error. WithLogger also applies to
// the inboxes.
func NewProjectTaskEventEndpointRunner(
	project *projectconfig.LoadedProject,
	connectionName string,
	routes []ProjectTaskEventTriggerRoute,
	options ...Option,
) (*TaskEventEndpointRunner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newTaskEventEndpointRunner(connection, project.Configuration, routes, func(
		key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[TaskEvent],
	) (sdkgo.TriggerTarget[TaskEvent], error) {
		inbox, err := project.TriggerInbox(key)
		if err != nil {
			return nil, err
		}
		return provider.NewDurableTriggerTarget(inbox, key, target, provider.WithTriggerLogger(connection.client.logger))
	})
}

// newTaskEventEndpointRunner runs routes on connection with the bindings that configuration stores.
func newTaskEventEndpointRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	routes []ProjectTaskEventTriggerRoute,
	makeDurable durableTaskEventTarget,
) (*TaskEventEndpointRunner, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("ClickUp taskEvent routes are required")
	}
	endpoint, err := connection.client.taskEventWebhookEndpoint(connection.reference)
	if err != nil {
		return nil, err
	}
	connectionName := connection.reference.Name
	triggerName := TaskEventTriggerDefinition.Trigger.TriggerName
	runners := make([]sdkgo.TriggerRunner, 0, len(routes))
	bindingNames := make(map[string]bool, len(routes))
	for _, route := range routes {
		bindingName := strings.TrimSpace(route.BindingName)
		if bindingName == "" || route.Target == nil {
			return nil, fmt.Errorf("ClickUp taskEvent binding name and target are required")
		}
		if bindingNames[bindingName] {
			return nil, fmt.Errorf("ClickUp taskEvent binding name %q is duplicated", bindingName)
		}
		bindingNames[bindingName] = true
		var bindingConfiguration TaskEventTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connectionName, triggerName, bindingName, &bindingConfiguration); err != nil {
			return nil, fmt.Errorf("ClickUp taskEvent binding %q configuration: %w", bindingName, err)
		}
		if err := bindingConfiguration.Validate(); err != nil {
			return nil, fmt.Errorf("binding %q: %w", bindingName, err)
		}
		key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: connectionName, TriggerName: triggerName, BindingName: bindingName}
		durableTarget, err := makeDurable(key, route.Target)
		if err != nil {
			return nil, err
		}
		runners = append(runners, NewTaskEventTrigger(TaskEventTriggerConfig{
			Connection: connection, ConnectionName: connectionName, BindingName: bindingName,
			Configuration: bindingConfiguration, Target: durableTarget,
		}))
	}
	endpointRunner, err := webhooktrigger.NewEndpointRunner(endpoint, runners...)
	if err != nil {
		return nil, err
	}
	return &TaskEventEndpointRunner{endpointRunner: endpointRunner, endpoint: endpoint}, nil
}

// ServeHTTP verifies, records, and acknowledges one ClickUp webhook request. It answers 503 until Run
// has replayed every binding's pending events and started receiving, so ClickUp retries.
func (runner *TaskEventEndpointRunner) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	runner.endpointRunner.ServeHTTP(response, request)
}

// Run replays each binding's durable inbox and then delivers new events until ctx ends or a binding
// fails. It returns ctx's error after cancellation.
func (runner *TaskEventEndpointRunner) Run(ctx context.Context) error {
	return runner.endpointRunner.Run(ctx)
}

// RunningSourceCount reports how many bindings are receiving events. A readiness check can wait until
// it equals the number of routes; until it is positive, every event is answered 503.
func (runner *TaskEventEndpointRunner) RunningSourceCount() int {
	return runner.endpoint.RunningSourceCount()
}
