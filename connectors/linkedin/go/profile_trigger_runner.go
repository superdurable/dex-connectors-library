// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// ProfileChangedTriggerRoute binds one profileChanged binding to its application target and cursor.
type ProfileChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration is the binding configuration; its zero value is the only one.
	Configuration ProfileChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[ProfileChangedEvent]
	// Cursor stores the binding's last profile, such as projectconfig.LoadedProject.PollCursorStore. Nil
	// keeps it in memory, so every process start records a new baseline and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// ProfileTriggerRunnerConfig configures every LinkedIn Trigger binding polled through one connection.
type ProfileTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// ProfileChangedRoutes lists the profileChanged bindings; at least one is required.
	ProfileChangedRoutes []ProfileChangedTriggerRoute
}

// NewProfileTriggerRunner creates one poll runner for the configured profileChanged bindings. Each binding
// watches only the connected member's own profile. Run replays each durable target, records the current
// profile as a baseline for a binding without a cursor, then reads LinkedIn UserInfo each pollInterval with
// jitter and delivers one change whenever the profile's claims differ from the stored ones. A stored cursor
// that is undecodable, or a connection re-authorized as another member, records a new baseline and logs a
// WARN record; differences in that gap are not delivered. A 429 pauses every binding of the connection for
// Retry-After, or backs off exponentially without it; other failures, including a 401 that a credential
// refresh did not fix, back off per binding. Delivery is at least once, and each change keeps one Trigger
// event ID across refetches.
func NewProfileTriggerRunner(config ProfileTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.ProfileChangedRoutes) == 0 {
		return nil, fmt.Errorf("LinkedIn profileChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.ProfileChangedRoutes))
	for _, route := range config.ProfileChangedRoutes {
		poller, err := newProfileChangedPoller(config.Connection, route)
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

// ProjectProfileChangedTriggerRoute selects one profileChanged binding saved in the project configuration.
type ProjectProfileChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[ProfileChangedEvent]
}

// ProjectProfileTriggerRunnerConfig selects LinkedIn Trigger bindings saved in the project configuration.
type ProjectProfileTriggerRunnerConfig struct {
	// ProfileChangedRoutes lists the profileChanged bindings; at least one is required.
	ProfileChangedRoutes []ProjectProfileChangedTriggerRoute
}

// NewProjectProfileTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// configuration (an unsaved binding uses the empty configuration), and creates a durable runner: each route
// records changes in its binding's project Trigger inbox before its stored profile commits in the binding's
// project poll cursor, so a restarted or replaced replica neither misses nor repeats a change. The
// WithLogger option also applies to those inboxes.
func NewProjectProfileTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectProfileTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableProfileTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableProfileTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectProfileTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := ProfileChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := ProfileTriggerRunnerConfig{Connection: connection}
	for _, route := range config.ProfileChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("LinkedIn profileChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration ProfileChangedTriggerConfiguration
		// An unsaved binding uses the empty configuration.
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if err != nil && !errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("linkedin profileChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.ProfileChangedRoutes = append(runnerConfig.ProfileChangedRoutes, ProfileChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewProfileTriggerRunner(runnerConfig)
}

func newProfileChangedPoller(connection Connection, route ProfileChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, fmt.Errorf("LinkedIn profileChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, profileChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[ProfileChangedEvent]{
		Definition: ProfileChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: ProfileChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newProfileFetcher(connection.client, connection.reference), Target: route.Target,
	})
}

// profileChangedTriggerSource serves the generated NewProfileChangedTrigger. It has no binding name or project
// storage, so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) profileChangedTriggerSource(connection sdkgo.ConnectionRef, configuration ProfileChangedTriggerConfiguration) sdkgo.TriggerSource[ProfileChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(profileChangedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[ProfileChangedEvent]{
		Definition: ProfileChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: ProfileChangedTriggerDefinition.Trigger, Name: ProfileChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newProfileFetcher(client, connection),
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
