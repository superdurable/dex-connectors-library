// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// CRMObjectChangedTriggerRoute binds one crmObjectChanged configuration to its application target and cursor.
type CRMObjectChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched object type and the returned properties.
	Configuration CRMObjectChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[CRMObjectChangedEvent]
	// Cursor stores the binding's search position, such as projectconfig.LoadedProject.PollCursorStore.
	// Nil keeps it in memory, so every process start records a new baseline and changes made while
	// stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// CRMObjectTriggerRunnerConfig configures every HubSpot Trigger binding polled through one connection.
type CRMObjectTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// CRMObjectChangedRoutes lists the crmObjectChanged bindings; at least one is required.
	CRMObjectChangedRoutes []CRMObjectChangedTriggerRoute
}

// NewCRMObjectTriggerRunner creates one poll runner for the configured crmObjectChanged bindings. Run
// replays each durable target, records a baseline for a binding without a cursor, then polls every binding
// each pollInterval with jitter, in configuration order, one search request at a time. A 429 or 423
// pauses every binding of the connection for Retry-After or X-HubSpot-RateLimit-Interval-Milliseconds,
// and an exhausted daily limit backs off exponentially up to 15 minutes; other failures back off per
// binding. A paging offset HubSpot rejects records a new baseline and logs a WARN record. Delivery is at
// least once, and each record version keeps one Trigger event ID across refetches.
func NewCRMObjectTriggerRunner(config CRMObjectTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.CRMObjectChangedRoutes) == 0 {
		return nil, errors.New("HubSpot crmObjectChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.CRMObjectChangedRoutes))
	for _, route := range config.CRMObjectChangedRoutes {
		poller, err := newCRMObjectChangedPoller(config.Connection, route)
		if err != nil {
			return nil, err
		}
		pollers = append(pollers, poller)
	}
	return pollingtrigger.NewRunner(pollingtrigger.RunnerConfig{
		Connection: config.Connection.reference, Schedule: config.Connection.client.pollSchedule(),
		Pollers: pollers, Logger: config.Connection.client.logger,
	})
}

// ProjectCRMObjectChangedTriggerRoute selects one crmObjectChanged binding saved in the project configuration.
type ProjectCRMObjectChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Properties lists the property names to return on each delivered record, as
	// CRMObjectChangedTriggerConfiguration.Properties; it applies when the saved binding names none.
	Properties []string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[CRMObjectChangedEvent]
}

// ProjectCRMObjectTriggerRunnerConfig selects HubSpot Trigger bindings saved in the project configuration.
type ProjectCRMObjectTriggerRunnerConfig struct {
	// CRMObjectChangedRoutes lists the crmObjectChanged bindings; at least one is required.
	CRMObjectChangedRoutes []ProjectCRMObjectChangedTriggerRoute
}

// NewProjectCRMObjectTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// object type, and creates a durable runner: each route records changes in its binding's project Trigger
// inbox before its search position commits in the binding's project poll cursor, so a restarted or
// replaced replica neither misses nor repeats a change. A binding without a saved object type is a
// configuration error. The WithLogger option also applies to those inboxes.
func NewProjectCRMObjectTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectCRMObjectTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableCRMObjectTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableCRMObjectTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectCRMObjectTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := CRMObjectChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := CRMObjectTriggerRunnerConfig{Connection: connection}
	for _, route := range config.CRMObjectChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("HubSpot crmObjectChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration CRMObjectChangedTriggerConfiguration
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("hubspot crmObjectChanged binding %q has no saved object type; pick one in Dex Web", route.BindingName)
		}
		if err != nil {
			return nil, fmt.Errorf("hubspot crmObjectChanged binding %q configuration: %w", route.BindingName, err)
		}
		if len(bindingConfiguration.Properties) == 0 {
			bindingConfiguration.Properties = route.Properties
		}
		key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: connection.reference.Name, TriggerName: triggerName, BindingName: route.BindingName}
		inbox, err := storage.TriggerInbox(key)
		if err != nil {
			return nil, err
		}
		target, err := provider.NewDurableTriggerTarget(inbox, key, route.Target, provider.WithTriggerLogger(connection.client.logger))
		if err != nil {
			return nil, err
		}
		cursor, err := storage.PollCursorStore(projectconfig.PollCursorKey(key))
		if err != nil {
			return nil, err
		}
		runnerConfig.CRMObjectChangedRoutes = append(runnerConfig.CRMObjectChangedRoutes, CRMObjectChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewCRMObjectTriggerRunner(runnerConfig)
}

func newCRMObjectChangedPoller(connection Connection, route CRMObjectChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, errors.New("HubSpot crmObjectChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, crmObjectChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[CRMObjectChangedEvent]{
		Definition: CRMObjectChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: CRMObjectChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newCRMObjectChangeFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
	})
}

// cRMObjectChangedTriggerSource serves the generated NewCRMObjectChangedTrigger. It has no project storage,
// so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) cRMObjectChangedTriggerSource(connection sdkgo.ConnectionRef, configuration CRMObjectChangedTriggerConfiguration) sdkgo.TriggerSource[CRMObjectChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(crmObjectChangedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[CRMObjectChangedEvent]{
		Definition: CRMObjectChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: CRMObjectChangedTriggerDefinition.Trigger, Name: CRMObjectChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newCRMObjectChangeFetcher(client, connection, configuration),
	}, client.pollSchedule())
	if err != nil {
		panic(err)
	}
	return source
}

func (client *Client) pollSchedule() pollingtrigger.PollSchedule {
	// A zero MaxBackoff uses the kit's cap, which grows with a long poll interval.
	return pollingtrigger.PollSchedule{Interval: client.pollInterval}
}
