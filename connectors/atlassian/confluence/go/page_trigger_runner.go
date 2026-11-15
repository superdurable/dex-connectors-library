// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// PageChangedTriggerRoute binds one pageChanged configuration to its application target and cursor.
type PageChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched space; blank watches every space the connection can view.
	Configuration PageChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[PageChangedEvent]
	// Cursor stores the binding's scan position and recently seen page versions, such as
	// projectconfig.LoadedProject.PollCursorStore. Nil keeps it in memory, so every process start records a
	// new baseline and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// PageTriggerRunnerConfig configures every Confluence Trigger binding polled through one connection.
type PageTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// PageChangedRoutes lists the pageChanged bindings; at least one is required.
	PageChangedRoutes []PageChangedTriggerRoute
}

// NewPageTriggerRunner creates one poll runner for the configured pageChanged bindings. Run replays each
// durable target, records a baseline for a binding without a cursor, then polls every binding each
// pollInterval with jitter. Each poll reads Confluence CQL search, newest modification first, back to ten
// minutes before the previous scan began, and delivers each page version not delivered before. A 429, or a
// 503 with Retry-After, pauses every binding of the connection for Retry-After, or backs off exponentially
// without it; other failures back off per binding up to 15 minutes. A cursor for another site or space
// records a new baseline and logs a WARN record. Delivery is at least once, and each page version keeps one
// Trigger event ID across refetches.
func NewPageTriggerRunner(config PageTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.PageChangedRoutes) == 0 {
		return nil, fmt.Errorf("Confluence pageChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.PageChangedRoutes))
	for _, route := range config.PageChangedRoutes {
		poller, err := newPageChangedPoller(config.Connection, route)
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

// ProjectPageChangedTriggerRoute selects one pageChanged binding saved in the project configuration.
type ProjectPageChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[PageChangedEvent]
}

// ProjectPageTriggerRunnerConfig selects Confluence Trigger bindings saved in the project configuration.
type ProjectPageTriggerRunnerConfig struct {
	// PageChangedRoutes lists the pageChanged bindings; at least one is required.
	PageChangedRoutes []ProjectPageChangedTriggerRoute
}

// NewProjectPageTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// space (an unsaved binding watches every space the connection can view), and creates a durable runner: each
// route records changes in its binding's project Trigger inbox before its scan position commits in the binding's
// project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. The
// WithLogger option also applies to those inboxes.
func NewProjectPageTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectPageTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurablePageTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurablePageTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectPageTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := PageChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := PageTriggerRunnerConfig{Connection: connection}
	for _, route := range config.PageChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Confluence pageChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration PageChangedTriggerConfiguration
		// An unsaved binding watches every visible space, as a blank picker does.
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if err != nil && !errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("confluence pageChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.PageChangedRoutes = append(runnerConfig.PageChangedRoutes, PageChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewPageTriggerRunner(runnerConfig)
}

func newPageChangedPoller(connection Connection, route PageChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, fmt.Errorf("Confluence pageChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, pageChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[PageChangedEvent]{
		Definition: PageChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: PageChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newPageChangeFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
		Snapshot: &pageChangeSnapshotOptions,
	})
}

// pageChangedTriggerSource serves the generated NewPageChangedTrigger. It has no binding name or project
// storage, so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) pageChangedTriggerSource(connection sdkgo.ConnectionRef, configuration PageChangedTriggerConfiguration) sdkgo.TriggerSource[PageChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(pageChangedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[PageChangedEvent]{
		Definition: PageChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: PageChangedTriggerDefinition.Trigger, Name: PageChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newPageChangeFetcher(client, connection, configuration),
		Snapshot: &pageChangeSnapshotOptions,
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
