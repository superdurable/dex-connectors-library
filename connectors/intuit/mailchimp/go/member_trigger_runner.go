// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// MemberChangedTriggerRoute binds one memberChanged configuration to its application target and cursor.
type MemberChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched audience and the delivered statuses.
	Configuration MemberChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[MemberChangedEvent]
	// Cursor stores the binding's position, such as projectconfig.LoadedProject.PollCursorStore. Nil keeps
	// it in memory, so every process start records a new baseline and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// MemberTriggerRunnerConfig configures every Mailchimp Trigger binding polled through one connection.
type MemberTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// MemberChangedRoutes lists the memberChanged bindings; at least one is required.
	MemberChangedRoutes []MemberChangedTriggerRoute
}

// NewMemberTriggerRunner creates one poll runner for the configured memberChanged bindings. Run replays
// each durable target, records a baseline for a binding without a cursor, then polls every binding each
// pollInterval with jitter, one request at a time, so the runner holds at most one of the 10 simultaneous
// requests Mailchimp allows each user. A 429, or a 403 without an error document, pauses every binding of
// the connection for Retry-After, or backs off exponentially without it; a 400 for the cursor's query
// records a new baseline and logs a WARN record; other failures back off per binding up to 15 minutes.
// Delivery is at least once, and each contact version keeps one Trigger event ID across refetches.
func NewMemberTriggerRunner(config MemberTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.MemberChangedRoutes) == 0 {
		return nil, errors.New("Mailchimp memberChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.MemberChangedRoutes))
	for _, route := range config.MemberChangedRoutes {
		poller, err := newMemberChangedPoller(config.Connection, route)
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

// ProjectMemberChangedTriggerRoute selects one memberChanged binding saved in the project configuration.
type ProjectMemberChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[MemberChangedEvent]
}

// ProjectMemberTriggerRunnerConfig selects Mailchimp Trigger bindings saved in the project configuration.
type ProjectMemberTriggerRunnerConfig struct {
	// MemberChangedRoutes lists the memberChanged bindings; at least one is required.
	MemberChangedRoutes []ProjectMemberChangedTriggerRoute
}

// NewProjectMemberTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// audience and statuses, and creates a durable runner: each route records changes in its binding's project
// Trigger inbox before its position commits in the binding's project poll cursor, so a restarted or
// replaced replica neither misses nor repeats a change. A binding without a saved audience is an error.
// The WithLogger option also applies to those inboxes.
func NewProjectMemberTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectMemberTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableMemberTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableMemberTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectMemberTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := MemberChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := MemberTriggerRunnerConfig{Connection: connection}
	for _, route := range config.MemberChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Mailchimp memberChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration MemberChangedTriggerConfiguration
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("mailchimp memberChanged binding %q has no saved audience; enter its audience ID in Dex Web Connections", route.BindingName)
		}
		if err != nil {
			return nil, fmt.Errorf("mailchimp memberChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.MemberChangedRoutes = append(runnerConfig.MemberChangedRoutes, MemberChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewMemberTriggerRunner(runnerConfig)
}

func newMemberChangedPoller(connection Connection, route MemberChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, errors.New("Mailchimp memberChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, memberChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[MemberChangedEvent]{
		Definition: MemberChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: MemberChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newMemberChangeFetcher(connection.client, connection.reference, route.Configuration),
		Target: route.Target, Snapshot: &memberSnapshotOptions,
	})
}

// memberChangedTriggerSource serves the generated NewMemberChangedTrigger. It has no project storage, so
// it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) memberChangedTriggerSource(connection sdkgo.ConnectionRef, configuration MemberChangedTriggerConfiguration) sdkgo.TriggerSource[MemberChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(memberChangedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[MemberChangedEvent]{
		Definition: MemberChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: MemberChangedTriggerDefinition.Trigger, Name: MemberChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newMemberChangeFetcher(client, connection, configuration),
		Snapshot: &memberSnapshotOptions,
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
