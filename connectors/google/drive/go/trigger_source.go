// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive

import (
	"context"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// standaloneBindingName identifies a generated per-Trigger factory's poller, which never learns its
// binding name; it appears only in log records and the in-memory cursor.
const standaloneBindingName = "standalone"

func (client *Client) fileCreatedTriggerSource(connection sdkgo.ConnectionRef, configuration FileTriggerConfiguration) sdkgo.TriggerSource[FileEvent] {
	return client.mustStandaloneFileTriggerSource(connection, fileCreatedTriggerName, configuration)
}

func (client *Client) fileUpdatedTriggerSource(connection sdkgo.ConnectionRef, configuration FileTriggerConfiguration) sdkgo.TriggerSource[FileEvent] {
	return client.mustStandaloneFileTriggerSource(connection, fileUpdatedTriggerName, configuration)
}

func (client *Client) fileTrashedTriggerSource(connection sdkgo.ConnectionRef, configuration FileTriggerConfiguration) sdkgo.TriggerSource[FileEvent] {
	return client.mustStandaloneFileTriggerSource(connection, fileTrashedTriggerName, configuration)
}

// mustStandaloneFileTriggerSource polls changes.list for one binding with an in-memory cursor, so a
// restart reseeds and misses changes made while stopped; NewProjectFileTriggerRunner is durable. It
// panics on invalid configuration, like the generated factories that call it.
func (client *Client) mustStandaloneFileTriggerSource(
	connection sdkgo.ConnectionRef,
	triggerName string,
	configuration FileTriggerConfiguration,
) sdkgo.TriggerSource[FileEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	definition := fileTriggerDefinition(triggerName)
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[FileEvent]{
		Definition: definition,
		Binding:    sdkgo.TriggerBindingRef{Connection: connection, Trigger: definition.Trigger, Name: standaloneBindingName},
		Cursor:     pollingtrigger.NewMemoryCursorStore(),
		Fetcher: &bindingFileEventFetcher{
			changes: newFileChangeFetcher(client, connection, triggerName), selectEvents: fileChangeSelector(triggerName, configuration),
		},
	}, pollingtrigger.PollSchedule{Interval: client.pollInterval})
	if err != nil {
		panic(err)
	}
	return source
}

// bindingFileEventFetcher turns the shared change feed into one binding's event feed.
type bindingFileEventFetcher struct {
	changes      *fileChangeFetcher
	selectEvents func(fileChange, *projectconfig.KeyedSnapshotEntry) []sdkgo.TriggerEvent[FileEvent]
}

// SeedCursor returns Drive's current change position.
func (fetcher *bindingFileEventFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	return fetcher.changes.SeedCursor(ctx)
}

// FetchChanges returns the binding's events from one changes.list page.
func (fetcher *bindingFileEventFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[FileEvent]], error) {
	page, err := fetcher.changes.FetchChanges(ctx, cursor)
	selected := pollingtrigger.ChangePage[sdkgo.TriggerEvent[FileEvent]]{Next: page.Next, HasMorePages: page.HasMorePages}
	if err != nil {
		return selected, err
	}
	for _, change := range page.Changes {
		selected.Changes = append(selected.Changes, fetcher.selectEvents(change, nil)...)
	}
	return selected, nil
}
