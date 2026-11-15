// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/sdk-go/dex"
)

// runItemTrigger is the application side: it routes every polled change to one itemSyncFlow. With a
// project, events are recorded in the binding's Trigger inbox before the cursor commits past them.
func runItemTrigger(ctx context.Context, project *projectconfig.LoadedProject, client *dex.Client, flow dex.Flow, api *changeLogClient, logger *slog.Logger) error {
	connection := sdkgo.ConnectionRef{Provider: "changelog", Name: ConnectionName}
	definition := sdkgo.MustTriggerBindingDefinition(itemChangedBindingDefinition).Definition
	binding := sdkgo.TriggerBindingRef{Connection: connection, Trigger: definition.Trigger, Name: itemChangedBindingDefinition.BindingName}
	key := projectconfig.TriggerInboxKey{
		ConnectorID: definition.Trigger.ConnectorID, ConnectionName: connection.Name,
		TriggerName: definition.Trigger.TriggerName, BindingName: binding.Name,
	}
	target := sdkgo.NewDexFlowTriggerTarget(client, flow,
		func(sdkgo.TriggerEvent[ItemChanged]) bool { return true },
		func(event sdkgo.TriggerEvent[ItemChanged]) string {
			return "item-sync-" + strings.ReplaceAll(event.ID, "@", "-")
		},
		func(event sdkgo.TriggerEvent[ItemChanged]) ItemChanged { return event.Payload },
		sdkgo.WithTriggerLogger(logger),
	)
	cursor := pollingtrigger.NewMemoryCursorStore()
	if project != nil {
		// Durable inbox: events are recorded here before the cursor moves past them.
		inbox, err := project.TriggerInbox(key)
		if err != nil {
			return err
		}
		if target, err = provider.NewDurableTriggerTarget(inbox, key, target, provider.WithTriggerLogger(logger)); err != nil {
			return err
		}
		if cursor, err = project.PollCursorStore(projectconfig.PollCursorKey(key)); err != nil {
			return err
		}
	}
	poller, err := pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[ItemChanged]{
		Definition: definition, Binding: binding, Cursor: cursor,
		Fetcher: &changeLogFetcher{api: api}, Target: target,
	})
	if err != nil {
		return err
	}
	runner, err := pollingtrigger.NewRunner(pollingtrigger.RunnerConfig{
		Connection: connection,
		Schedule:   pollingtrigger.PollSchedule{Interval: 2 * time.Second, MaxBackoff: time.Minute},
		Pollers:    []pollingtrigger.Poller{poller},
		Logger:     logger,
	})
	if err != nil {
		return err
	}
	// Replays the inbox, seeds on first start, then polls until ctx ends.
	return runner.Run(ctx)
}
