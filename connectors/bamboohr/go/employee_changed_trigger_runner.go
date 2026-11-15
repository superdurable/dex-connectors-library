// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// EmployeeChangedTriggerRoute binds one employeeChanged configuration to its application target and cursor.
type EmployeeChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the change type the binding delivers.
	Configuration EmployeeChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[EmployeeChangedEvent]
	// Cursor stores the binding's position, such as projectconfig.LoadedProject.PollCursorStore. Nil keeps it
	// in memory, so every process start begins at the current time and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// EmployeeChangedTriggerRunnerConfig configures every employeeChanged binding polled through one connection.
type EmployeeChangedTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// Routes lists the employeeChanged bindings, polled in this order when several are due; at least one
	// is required.
	Routes []EmployeeChangedTriggerRoute
	// RequestLimiter optionally paces every poll request, for example to share BambooHR's throttling budget
	// with other work of the same process.
	RequestLimiter pollingtrigger.RequestLimiter
}

// NewEmployeeChangedTriggerRunner creates one poll runner for the configured employeeChanged bindings. It
// makes no provider request and reads no storage.
//
// Run replays each durable target, starts a binding without a cursor at BambooHR's current time, then
// polls every binding each pollInterval with jitter. Each poll makes one GET /employees/changed request
// and delivers the changes after the cursor that are at least 30 seconds old by BambooHR's Date header,
// oldest first, at most 500 per page; further pages are read
// at once, up to the kit's page cap per poll. A 429 or 503 pauses every binding of the connection for
// Retry-After, or backs off exponentially without it. A 400 rejecting the cursor starts the binding again
// at the current time and logs a WARN record; changes in between are not delivered. Other failures back off
// per binding. Delivery is at least once, and each change keeps one Trigger event ID across refetches.
func NewEmployeeChangedTriggerRunner(config EmployeeChangedTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.Routes) == 0 {
		return nil, errors.New("BambooHR employeeChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.Routes))
	for _, route := range config.Routes {
		poller, err := newEmployeeChangedPoller(config.Connection, route)
		if err != nil {
			return nil, err
		}
		pollers = append(pollers, poller)
	}
	return pollingtrigger.NewRunner(pollingtrigger.RunnerConfig{
		Connection: config.Connection.reference, Schedule: config.Connection.client.employeeChangedPollSchedule(),
		Pollers: pollers, RequestLimiter: config.RequestLimiter, Logger: config.Connection.client.logger,
	})
}

// ProjectEmployeeChangedTriggerRoute selects one employeeChanged binding saved in the project configuration.
type ProjectEmployeeChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[EmployeeChangedEvent]
}

// NewProjectEmployeeChangedTriggerRunner opens connectionName from the loaded project, loads each binding's
// saved change type (an unsaved binding delivers every change), and creates a durable runner: each route
// records changes in its binding's project Trigger inbox before its cursor commits in the binding's project
// poll cursor, so a restarted or replaced replica neither misses nor repeats a change. The WithLogger option
// also applies to those inboxes.
func NewProjectEmployeeChangedTriggerRunner(
	project *projectconfig.LoadedProject,
	connectionName string,
	routes []ProjectEmployeeChangedTriggerRoute,
	options ...Option,
) (*pollingtrigger.Runner, error) {
	if project == nil {
		return nil, errors.New("BambooHR employeeChanged runner requires the loaded project configuration")
	}
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableEmployeeChangedTriggerRunner(connection, project.Configuration, project, routes)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableEmployeeChangedTriggerRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	storage durableTriggerStorage,
	routes []ProjectEmployeeChangedTriggerRoute,
) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := EmployeeChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := EmployeeChangedTriggerRunnerConfig{Connection: connection}
	for _, route := range routes {
		if route.Target == nil {
			return nil, fmt.Errorf("BambooHR employeeChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration EmployeeChangedTriggerConfiguration
		// An unsaved binding delivers every change, as an empty changeTypePicker does.
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if err != nil && !errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("bamboohr employeeChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.Routes = append(runnerConfig.Routes, EmployeeChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewEmployeeChangedTriggerRunner(runnerConfig)
}

func newEmployeeChangedPoller(connection Connection, route EmployeeChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, errors.New("BambooHR employeeChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, employeeChangedConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[EmployeeChangedEvent]{
		Definition: EmployeeChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: EmployeeChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newEmployeeChangeFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
	})
}

// employeeChangedTriggerSource serves the generated NewEmployeeChangedTrigger. It has no project storage, so
// it keeps the cursor in memory and starts at the current time on every start.
func (client *Client) employeeChangedTriggerSource(connection sdkgo.ConnectionRef, configuration EmployeeChangedTriggerConfiguration) sdkgo.TriggerSource[EmployeeChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(employeeChangedConfigurationError(err))
	}
	// The generated factory passes no binding name, so log records name the Trigger instead.
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[EmployeeChangedEvent]{
		Definition: EmployeeChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: EmployeeChangedTriggerDefinition.Trigger, Name: EmployeeChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newEmployeeChangeFetcher(client, connection, configuration),
	}, client.employeeChangedPollSchedule())
	if err != nil {
		panic(err)
	}
	return source
}

func (client *Client) employeeChangedPollSchedule() pollingtrigger.PollSchedule {
	// A zero MaxBackoff uses the kit's cap, which grows with a long poll interval.
	return pollingtrigger.PollSchedule{Interval: client.pollInterval}
}
