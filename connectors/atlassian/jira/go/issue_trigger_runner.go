// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// IssueChangedTriggerRoute binds one issueChanged configuration to its application target and cursor.
type IssueChangedTriggerRoute struct {
	// BindingName is the stable application binding name, unique within the runner.
	BindingName string
	// Configuration selects the watched project; blank watches every project the account can browse.
	Configuration IssueChangedTriggerConfiguration
	// Target routes each change into the application. A target that implements
	// sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records each change before
	// the cursor commits past it; any other target consumes it first.
	Target sdkgo.TriggerTarget[IssueChangedEvent]
	// Cursor stores the binding's position, such as projectconfig.LoadedProject.PollCursorStore. Nil keeps
	// it in memory, so every process start records a new baseline and changes made while stopped are lost.
	Cursor pollingtrigger.CursorStore
}

// IssueTriggerRunnerConfig configures every Jira Trigger binding polled through one connection.
type IssueTriggerRunnerConfig struct {
	// Connection is a connection opened with NewConnection or NewProjectConnection.
	Connection Connection
	// IssueChangedRoutes lists the issueChanged bindings; at least one is required.
	IssueChangedRoutes []IssueChangedTriggerRoute
}

// NewIssueTriggerRunner creates one poll runner for the configured issueChanged bindings. Run replays each
// durable target, records a baseline for a binding without a cursor, then polls every binding each
// pollInterval with jitter. A 429, including an exhausted hourly points quota, pauses every binding of the
// connection for Retry-After, or backs off exponentially without it; other failures back off per binding
// up to 15 minutes, and none of them moves the cursor. Delivery is at least once, and each issue version
// keeps one Trigger event ID across refetches.
func NewIssueTriggerRunner(config IssueTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := config.Connection.validate(); err != nil {
		return nil, err
	}
	if len(config.IssueChangedRoutes) == 0 {
		return nil, fmt.Errorf("Jira issueChanged routes are required")
	}
	pollers := make([]pollingtrigger.Poller, 0, len(config.IssueChangedRoutes))
	for _, route := range config.IssueChangedRoutes {
		poller, err := newIssueChangedPoller(config.Connection, route)
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

// ProjectIssueChangedTriggerRoute selects one issueChanged binding saved in the project configuration.
type ProjectIssueChangedTriggerRoute struct {
	// BindingName names the saved binding.
	BindingName string
	// Target routes each change into the application, usually sdkgo.NewDexFlowTriggerTarget.
	Target sdkgo.TriggerTarget[IssueChangedEvent]
}

// ProjectIssueTriggerRunnerConfig selects Jira Trigger bindings saved in the project configuration.
type ProjectIssueTriggerRunnerConfig struct {
	// IssueChangedRoutes lists the issueChanged bindings; at least one is required.
	IssueChangedRoutes []ProjectIssueChangedTriggerRoute
}

// NewProjectIssueTriggerRunner opens connectionName from the loaded project, loads each binding's saved
// project (an unsaved binding watches every project the account can browse), and creates a durable
// runner: each route records changes in its binding's project Trigger inbox before its position commits
// in the binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a
// change. The WithLogger option also applies to those inboxes.
func NewProjectIssueTriggerRunner(project *projectconfig.LoadedProject, connectionName string, config ProjectIssueTriggerRunnerConfig, options ...Option) (*pollingtrigger.Runner, error) {
	connection, err := NewProjectConnection(project, connectionName, options...)
	if err != nil {
		return nil, err
	}
	return newDurableIssueTriggerRunner(connection, project.Configuration, project, config)
}

// durableTriggerStorage opens one binding's inbox and cursor; projectconfig.LoadedProject implements it.
type durableTriggerStorage interface {
	TriggerInbox(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error)
	PollCursorStore(projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error)
}

func newDurableIssueTriggerRunner(connection Connection, configuration projectconfig.Configuration, storage durableTriggerStorage, config ProjectIssueTriggerRunnerConfig) (*pollingtrigger.Runner, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	triggerName := IssueChangedTriggerDefinition.Trigger.TriggerName
	runnerConfig := IssueTriggerRunnerConfig{Connection: connection}
	for _, route := range config.IssueChangedRoutes {
		if route.Target == nil {
			return nil, fmt.Errorf("Jira issueChanged binding %q target is required", route.BindingName)
		}
		var bindingConfiguration IssueChangedTriggerConfiguration
		// An unsaved binding watches every browsable project, as a blank picker does.
		err := configuration.DecodeTriggerConfiguration(ConnectorID, connection.reference.Name, triggerName, route.BindingName, &bindingConfiguration)
		if err != nil && !errors.Is(err, projectconfig.ErrObjectNotFound) {
			return nil, fmt.Errorf("jira issueChanged binding %q configuration: %w", route.BindingName, err)
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
		runnerConfig.IssueChangedRoutes = append(runnerConfig.IssueChangedRoutes, IssueChangedTriggerRoute{
			BindingName: route.BindingName, Configuration: bindingConfiguration, Target: target, Cursor: cursor,
		})
	}
	return NewIssueTriggerRunner(runnerConfig)
}

func newIssueChangedPoller(connection Connection, route IssueChangedTriggerRoute) (pollingtrigger.Poller, error) {
	if strings.TrimSpace(route.BindingName) == "" || route.Target == nil {
		return nil, fmt.Errorf("Jira issueChanged binding name and target are required")
	}
	if err := route.Configuration.Validate(); err != nil {
		return nil, issueChangedTriggerConfigurationError(err)
	}
	cursor := route.Cursor
	if cursor == nil {
		cursor = pollingtrigger.NewMemoryCursorStore()
	}
	return pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[IssueChangedEvent]{
		Definition: IssueChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection.reference, Trigger: IssueChangedTriggerDefinition.Trigger, Name: route.BindingName},
		Cursor:     cursor, Fetcher: newIssueChangeFetcher(connection.client, connection.reference, route.Configuration), Target: route.Target,
	})
}

// issueChangedTriggerSource serves the generated NewIssueChangedTrigger. It has no binding name or project
// storage, so it keeps the cursor in memory and records a new baseline on every start.
func (client *Client) issueChangedTriggerSource(connection sdkgo.ConnectionRef, configuration IssueChangedTriggerConfiguration) sdkgo.TriggerSource[IssueChangedEvent] {
	if err := configuration.Validate(); err != nil {
		panic(issueChangedTriggerConfigurationError(err))
	}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[IssueChangedEvent]{
		Definition: IssueChangedTriggerDefinition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: IssueChangedTriggerDefinition.Trigger, Name: IssueChangedTriggerDefinition.Trigger.TriggerName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(), Fetcher: newIssueChangeFetcher(client, connection, configuration),
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
