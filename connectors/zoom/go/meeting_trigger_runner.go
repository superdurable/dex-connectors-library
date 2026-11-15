// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// MeetingChangedTriggerRoute binds one meetingChanged configuration to its application target and cursor.
type MeetingChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration narrows the delivered change kinds; the zero value delivers every kind.
	Configuration MeetingChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[MeetingChangedEvent]
	// Cursor stores the binding's meeting snapshot, such as projectconfig.LoadedProject.PollCursorStore. Nil
	// keeps it in memory, so every process start records a new baseline and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// MeetingTriggerRunnerConfig configures every Zoom Trigger binding polled through one connection.
type MeetingTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// MeetingChangedRoutes lists the meetingChanged bindings; at least one is required.
	MeetingChangedRoutes []MeetingChangedTriggerRoute
}

// NewMeetingTriggerRunner creates one poll runner for the configured meetingChanged bindings. Run replays each
// durable target, records the current meeting list as a baseline for a binding without a cursor, then lists
// every page of the authorized user's scheduled meetings each pollInterval with jitter and delivers the
// differences from the stored snapshot. A stored cursor that is undecodable or holds another Zoom user's
// meetings records a new baseline and logs a WARN record; differences in that gap are not delivered. A 429,
// including Zoom's daily-limit 429, pauses every binding of the connection for Retry-After, or backs off
// exponentially without it; other failures back off per binding up to 15 minutes. Delivery is at least
// once, and each change keeps one Trigger event ID across refetches.
func NewMeetingTriggerRunner(config MeetingTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.MeetingChangedRoutes) == 0 {
		return nil, fmt.Errorf("Zoom meetingChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.MeetingChangedRoutes))
	for _, route := range config.MeetingChangedRoutes {
		poller, err := newMeetingChangedPoller(config.Connection, route)
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

// ProjectMeetingChangedTriggerRoute selects one meetingChanged binding saved in the project configuration.
type ProjectMeetingChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[MeetingChangedEvent]
}

// ProjectMeetingTriggerRunnerConfig selects Zoom Trigger bindings saved in the project configuration.
type ProjectMeetingTriggerRunnerConfig struct {
	// MeetingChangedRoutes lists the meetingChanged bindings; at least one is required.
	MeetingChangedRoutes []ProjectMeetingChangedTriggerRoute
}

// NewProjectMeetingTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// configuration (an unsaved binding delivers every change kind), and creates a durable runner: each route
// records changes in its binding's project Trigger inbox before its meeting snapshot commits in the binding's
// project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. The
// WithLogger option also applies to those inboxes.
func NewProjectMeetingTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectMeetingTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableMeetingTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableMeetingTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectMeetingTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := MeetingChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := MeetingTriggerRunnerConfig{Connection: connection}
	for _, route := range config.MeetingChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Zoom meetingChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration MeetingChangedTriggerConfiguration
		// An unsaved binding delivers every change kind.
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if err != nil && !errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("zoom meetingChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.MeetingChangedRoutes = append(runnerConfig.MeetingChangedRoutes, MeetingChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewMeetingTriggerRunner(runnerConfig)
}

func newMeetingChangedPoller(connection Connection, route MeetingChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, fmt.Errorf("Zoom meetingChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, meetingChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[MeetingChangedEvent]{
		Definition: MeetingChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: MeetingChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newMeetingListFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
		Snapshot: &meetingSnapshotOptions,
	})
}

// meetingChangedTriggerSource serves the generated NewMeetingChangedTrigger. It has no binding name or project
// storage, so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) meetingChangedTriggerSource(connection sdkgo.ConnectionRef, configuration MeetingChangedTriggerConfiguration) sdkgo.TriggerSource[MeetingChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(meetingChangedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[MeetingChangedEvent]{
		Definition: MeetingChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: MeetingChangedTriggerDefinition.Trigger, Name: MeetingChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newMeetingListFetcher(client, connection, configuration),
		Snapshot: &meetingSnapshotOptions,
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
