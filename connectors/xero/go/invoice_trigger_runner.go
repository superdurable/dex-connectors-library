// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// InvoiceChangedTriggerRoute binds one invoiceChanged configuration to its application target and cursor.
type InvoiceChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the invoice type and statuses; the zero value receives every invoice and bill.
	Configuration InvoiceChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[InvoiceChangedEvent]
	// Cursor stores the binding's poll position, such as projectconfig.LoadedProject.PollCursorStore. Nil keeps
	// it in memory, so every process start records a new baseline and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// InvoiceTriggerRunnerConfig configures every Xero Trigger binding polled through one connection.
type InvoiceTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// InvoiceChangedRoutes lists the invoiceChanged bindings; at least one is required.
	InvoiceChangedRoutes []InvoiceChangedTriggerRoute
}

// NewInvoiceTriggerRunner creates one poll runner for the configured invoiceChanged bindings. Run replays
// each durable target, records a baseline for a binding without a cursor, then polls every binding each
// pollInterval with jitter. A changed filter or organisation records a new baseline and logs a WARN
// record; changes made between the last poll and the new baseline are not delivered. A 429 for Xero's
// minute, concurrency, or daily limit pauses every binding of the connection for Retry-After, or backs off
// exponentially without it; other failures back off per binding up to 15 minutes. Delivery is at least
// once, and each change keeps one Trigger event ID across refetches.
func NewInvoiceTriggerRunner(config InvoiceTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.InvoiceChangedRoutes) == 0 {
		return nil, fmt.Errorf("Xero invoiceChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.InvoiceChangedRoutes))
	for _, route := range config.InvoiceChangedRoutes {
		poller, err := newInvoiceChangedPoller(config.Connection, route)
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

// ProjectInvoiceChangedTriggerRoute selects one invoiceChanged binding saved in the project configuration.
type ProjectInvoiceChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[InvoiceChangedEvent]
}

// ProjectInvoiceTriggerRunnerConfig selects Xero Trigger bindings saved in the project configuration.
type ProjectInvoiceTriggerRunnerConfig struct {
	// InvoiceChangedRoutes lists the invoiceChanged bindings; at least one is required.
	InvoiceChangedRoutes []ProjectInvoiceChangedTriggerRoute
}

// NewProjectInvoiceTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// invoice filter (an unsaved binding receives every invoice and bill), and creates a durable runner: each
// route records changes in its binding's project Trigger inbox before its position commits in the binding's
// project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. The
// WithLogger option also applies to those inboxes.
func NewProjectInvoiceTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectInvoiceTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableInvoiceTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableInvoiceTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectInvoiceTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := InvoiceChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := InvoiceTriggerRunnerConfig{Connection: connection}
	for _, route := range config.InvoiceChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Xero invoiceChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration InvoiceChangedTriggerConfiguration
		// An unsaved binding receives every invoice and bill, as a blank filter does.
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if err != nil && !errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("xero invoiceChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.InvoiceChangedRoutes = append(runnerConfig.InvoiceChangedRoutes, InvoiceChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewInvoiceTriggerRunner(runnerConfig)
}

func newInvoiceChangedPoller(connection Connection, route InvoiceChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, fmt.Errorf("Xero invoiceChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, invoiceChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[InvoiceChangedEvent]{
		Definition: InvoiceChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: InvoiceChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newInvoiceChangeFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
	})
}

// invoiceChangedTriggerSource serves the generated NewInvoiceChangedTrigger. It has no binding name or project
// storage, so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) invoiceChangedTriggerSource(connection sdkgo.ConnectionRef, configuration InvoiceChangedTriggerConfiguration) sdkgo.TriggerSource[InvoiceChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(invoiceChangedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[InvoiceChangedEvent]{
		Definition: InvoiceChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: InvoiceChangedTriggerDefinition.Trigger, Name: InvoiceChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newInvoiceChangeFetcher(client, connection, configuration),
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
