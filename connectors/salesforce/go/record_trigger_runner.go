// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// RecordModifiedPolledTriggerRoute binds one recordModifiedPolled configuration to its target and cursor.
type RecordModifiedPolledTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched object and the extra fields read with each record.
	Configuration RecordModifiedTriggerConfiguration
	// Target routes each record change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[RecordModifiedEvent]
	// Cursor stores the binding's (SystemModstamp, Id) position, such as
	// projectconfig.LoadedProject.PollCursorStore. Nil keeps it in memory, so every process start records a
	// new baseline and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// RecordTriggerRunnerConfig configures every Salesforce Trigger binding polled through one connection.
type RecordTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// RecordModifiedPolledRoutes lists the recordModifiedPolled bindings; at least one is required.
	RecordModifiedPolledRoutes []RecordModifiedPolledTriggerRoute
}

// NewRecordTriggerRunner creates one poll runner for the configured recordModifiedPolled bindings. Run
// replays each durable target, records a baseline for a binding without a cursor, then polls every binding
// each pollInterval with jitter, reading at most pollPageSize records per request and up to ten requests
// per poll while a backlog remains. REQUEST_LIMIT_EXCEEDED or HTTP 429 pauses every binding of the
// connection for Retry-After, or backs off exponentially without it; other failures, such as an object or
// field the user cannot read, back off per binding up to 15 minutes. Delivery is at least once, and each
// record change keeps one Trigger event ID across refetches.
func NewRecordTriggerRunner(config RecordTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.RecordModifiedPolledRoutes) == 0 {
		return nil, errors.New("Salesforce recordModifiedPolled routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.RecordModifiedPolledRoutes))
	for _, route := range config.RecordModifiedPolledRoutes {
		poller, err := newRecordModifiedPolledPoller(config.Connection, route)
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

// ProjectRecordModifiedPolledTriggerRoute selects one recordModifiedPolled binding saved in the project
// configuration.
type ProjectRecordModifiedPolledTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each record change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[RecordModifiedEvent]
}

// ProjectRecordTriggerRunnerConfig selects Salesforce Trigger bindings saved in the project configuration.
type ProjectRecordTriggerRunnerConfig struct {
	// RecordModifiedPolledRoutes lists the recordModifiedPolled bindings; at least one is required.
	RecordModifiedPolledRoutes []ProjectRecordModifiedPolledTriggerRoute
}

// NewProjectRecordTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// object and fields, and creates a durable runner: each route records changes in its binding's project
// Trigger inbox before its cursor commits in the binding's project poll cursor, so a restarted or replaced
// replica neither misses nor repeats a change. A binding without a saved object fails. The WithLogger
// option also applies to those inboxes.
func NewProjectRecordTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectRecordTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableRecordTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableRecordTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectRecordTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := RecordModifiedPolledTriggerDefinition.Trigger.TriggerName
	runnerConfig := RecordTriggerRunnerConfig{Connection: connection}
	for _, route := range config.RecordModifiedPolledRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Salesforce recordModifiedPolled binding %q target is required", route.BindingName)
		}
		var bindingConfiguration RecordModifiedTriggerConfiguration
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("salesforce recordModifiedPolled binding %q has no saved object; choose one in Dex Web Connections", route.BindingName)
		}
		if err != nil {
			return nil, fmt.Errorf("salesforce recordModifiedPolled binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.RecordModifiedPolledRoutes = append(runnerConfig.RecordModifiedPolledRoutes, RecordModifiedPolledTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewRecordTriggerRunner(runnerConfig)
}

func newRecordModifiedPolledPoller(connection Connection, route RecordModifiedPolledTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, errors.New("Salesforce recordModifiedPolled binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, recordModifiedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[RecordModifiedEvent]{
		Definition: RecordModifiedPolledTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: RecordModifiedPolledTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newRecordPollFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
	})
}

// recordModifiedPolledTriggerSource serves the generated NewRecordModifiedPolledTrigger. It has no project
// storage, so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) recordModifiedPolledTriggerSource(connection sdkgo.ConnectionRef, configuration RecordModifiedTriggerConfiguration) sdkgo.TriggerSource[RecordModifiedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(recordModifiedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[RecordModifiedEvent]{
		Definition: RecordModifiedPolledTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: RecordModifiedPolledTriggerDefinition.Trigger, Name: RecordModifiedPolledTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newRecordPollFetcher(client, connection, configuration),
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
