// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// FileTriggerRoute binds one fileCreated, fileUpdated, or fileTrashed binding to its application target.
type FileTriggerRoute struct {
	// BindingName names this configured Trigger binding; it is unique per Trigger.
	BindingName string
	// Configuration filters the files this binding receives.
	Configuration FileTriggerConfiguration
	// Target routes accepted events into the application, such as sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[FileEvent]
}

// FileTriggerRunnerConfig configures every file Trigger binding of one connection on one shared poller.
type FileTriggerRunnerConfig struct {
	// Connection is the Drive connection from NewProjectConnection or NewConnection.
	Connection Connection
	// Cursor stores the shared changes.list position. Use pollingtrigger.NewMemoryCursorStore in tests;
	// NewProjectFileTriggerRunner uses the project's durable feed cursor.
	Cursor pollingtrigger.CursorStore
	// FileCreatedRoutes receive files created after the cursor was first seeded.
	FileCreatedRoutes []FileTriggerRoute
	// FileUpdatedRoutes receive existing files whose modified time advanced.
	FileUpdatedRoutes []FileTriggerRoute
	// FileTrashedRoutes receive files moved to the trash.
	FileTrashedRoutes []FileTriggerRoute
}

// FileTriggerRunner reads the connection's Drive change log once per poll for every file Trigger
// binding and delivers each binding's matching events in change order.
type FileTriggerRunner struct {
	runner *pollingtrigger.Runner
}

// NewFileTriggerRunner validates config and builds one runner with one shared changes.list feed. It
// reads and writes nothing until Run. It fails for a missing cursor or target, no routes, a duplicated
// binding name within one Trigger, or an invalid configuration.
func NewFileTriggerRunner(config FileTriggerRunnerConfig) (*FileTriggerRunner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if config.Cursor == nil {
		return nil, errors.New("Google Drive file Trigger cursor store is required")
	}
	client, connection := config.Connection.client, config.Connection.reference
	var members []pollingtrigger.FeedMember[fileChange]
	for _, group := range []struct {
		triggerName string
		routes      []FileTriggerRoute
	}{
		{fileCreatedTriggerName, config.FileCreatedRoutes},
		{fileUpdatedTriggerName, config.FileUpdatedRoutes},
		{fileTrashedTriggerName, config.FileTrashedRoutes},
	} {
		for _, route := range group.routes {
			member, err := newFileFeedMember(connection, group.triggerName, route)
			if err != nil {
				return nil, err
			}
			members = append(members, member)
		}
	}
	if len(members) == 0 {
		return nil, errors.New("Google Drive file Trigger routes are required")
	}
	poller, err := pollingtrigger.NewFeedPoller(pollingtrigger.FeedPollerConfig[fileChange]{
		Name: fileChangesFeedName, Cursor: config.Cursor, Members: members,
		Fetcher: newFileChangeFetcher(client, connection, fileChangesFeedName),
	})
	if err != nil {
		return nil, err
	}
	runner, err := pollingtrigger.NewRunner(pollingtrigger.RunnerConfig{
		Connection: connection, Schedule: pollingtrigger.PollSchedule{Interval: client.pollInterval},
		Pollers: []pollingtrigger.Poller{poller}, Logger: client.logger,
	})
	if err != nil {
		return nil, err
	}
	return &FileTriggerRunner{runner: runner}, nil
}

// Run replays every durable target, seeds the cursor at Drive's current position on first start
// without delivering existing files, and then polls until ctx ends. It returns ctx.Err() on
// cancellation, or an error for a stored cursor document that can never be decoded. Delivery is at
// least once with stable event IDs, so a Dex Flow target starts one Flow per event.
func (runner *FileTriggerRunner) Run(ctx context.Context) error {
	return runner.runner.Run(ctx)
}

// ProjectFileTriggerRoute binds one file Trigger binding saved in the project configuration to its target.
type ProjectFileTriggerRoute struct {
	// BindingName names the saved Trigger binding.
	BindingName string
	// Target routes accepted events into the application.
	Target sdkgo.TriggerTarget[FileEvent]
}

// ProjectFileTriggerRunnerConfig selects file Trigger bindings saved in the project configuration.
type ProjectFileTriggerRunnerConfig struct {
	// FileCreatedRoutes select saved fileCreated bindings.
	FileCreatedRoutes []ProjectFileTriggerRoute
	// FileUpdatedRoutes select saved fileUpdated bindings.
	FileUpdatedRoutes []ProjectFileTriggerRoute
	// FileTrashedRoutes select saved fileTrashed bindings.
	FileTrashedRoutes []ProjectFileTriggerRoute
}

// NewProjectFileTriggerRunner opens connectionName from the loaded project configuration, decodes each
// selected binding's saved configuration, and builds one durable runner. Each route records events in
// its binding's project Trigger inbox before the shared feed cursor, stored beside the inboxes, commits
// past them, so a restarted or replaced replica neither misses nor replays a change. A binding without
// saved configuration is an error. The WithLogger option also applies to the inboxes.
func NewProjectFileTriggerRunner(
	project *projectconfig.LoadedProject,
	connectionName string,
	config ProjectFileTriggerRunnerConfig,
	options ...Option,
) (*FileTriggerRunner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	cursor, err := project.PollFeedCursorStore(projectconfig.PollFeedKey{
		ConnectorID: ConnectorID, ConnectionName: connectionName, FeedName: fileChangesFeedName,
	})
	if err != nil {
		return nil, err
	}
	return newDurableFileTriggerRunner(connection, project.Configuration, project.TriggerInbox, cursor, config)
}

// newDurableFileTriggerRunner wraps each route's target in the inbox that openInbox opens for its binding.
func newDurableFileTriggerRunner(
	connection Connection,
	configuration projectconfig.Configuration,
	openInbox func(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error),
	cursor pollingtrigger.CursorStore,
	config ProjectFileTriggerRunnerConfig,
) (*FileTriggerRunner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	runnerConfig := FileTriggerRunnerConfig{Connection: connection, Cursor: cursor}
	var err error
	if runnerConfig.FileCreatedRoutes, err = resolveProjectFileTriggerRoutes(connection, configuration, openInbox, fileCreatedTriggerName, config.FileCreatedRoutes); err != nil {
		return nil, err
	}
	if runnerConfig.FileUpdatedRoutes, err = resolveProjectFileTriggerRoutes(connection, configuration, openInbox, fileUpdatedTriggerName, config.FileUpdatedRoutes); err != nil {
		return nil, err
	}
	if runnerConfig.FileTrashedRoutes, err = resolveProjectFileTriggerRoutes(connection, configuration, openInbox, fileTrashedTriggerName, config.FileTrashedRoutes); err != nil {
		return nil, err
	}
	return NewFileTriggerRunner(runnerConfig)
}

// resolveProjectFileTriggerRoutes decodes each saved binding and wraps its target in the binding's inbox.
func resolveProjectFileTriggerRoutes(
	connection Connection,
	configuration projectconfig.Configuration,
	openInbox func(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error),
	triggerName string,
	routes []ProjectFileTriggerRoute,
) ([]FileTriggerRoute, error) {
	resolved := make([]FileTriggerRoute, 0, len(routes))
	for _, route := range routes {
		var saved FileTriggerConfiguration
		if err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &saved); err != nil {
			return nil, fmt.Errorf("google-drive %s binding %q configuration: %w", triggerName, route.BindingName, err)
		}
		key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: connection.reference.Name, TriggerName: triggerName, BindingName: route.BindingName}
		inbox, err := openInbox(key)
		if err != nil {
			return nil, err
		}
		target, err := provider.NewDurableTriggerTarget(inbox, key, route.Target, provider.WithTriggerLogger(connection.client.logger))
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, FileTriggerRoute{BindingName: route.BindingName, Configuration: saved, Target: target})
	}
	return resolved, nil
}

func newFileFeedMember(connection sdkgo.ConnectionRef, triggerName string, route FileTriggerRoute) (pollingtrigger.FeedMember[fileChange], error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, errors.New("Google Drive file Trigger binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, fmt.Errorf("google-drive %s binding %q: %w", triggerName, route.BindingName, err)
	}
	definition := fileTriggerDefinition(triggerName)
	binding := sdkgo.TriggerBindingRef{Connection: connection, Trigger: definition.Trigger, Name: route.BindingName}
	return pollingtrigger.NewFeedMember(definition, binding, fileChangeSelector(triggerName, route.Configuration), route.Target)
}

func fileTriggerDefinition(triggerName string) sdkgo.TriggerDefinition {
	switch triggerName {
	case fileCreatedTriggerName:
		return FileCreatedTriggerDefinition
	case fileUpdatedTriggerName:
		return FileUpdatedTriggerDefinition
	}
	return FileTrashedTriggerDefinition
}
