// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const (
	itemChangedOperationID = "itemChanged"
	// itemPollPageSize is the items_page limit of every poll request, the connector's listItems bound, so one
	// page of full items stays inside the default response limit.
	itemPollPageSize = MaxListItemsLimit
	// maximumItemScanPages bounds one poll's newest-first scan at 10,000 items.
	maximumItemScanPages = 100
	// itemChangeLookback rescans items updated shortly before the cursor, so a late or same-second update
	// still arrives; the snapshot keeps them from arriving twice.
	itemChangeLookback = 5 * time.Minute
	// maximumItemChangesPerPage and maximumItemChangeBatchBytes keep one recorded batch of full items well
	// below the durable Trigger inbox's 1000 events and 8 MiB, even with a raised maxResponseBytes.
	maximumItemChangesPerPage   = 100
	maximumItemChangeBatchBytes = 4 << 20
)

// itemChangedSnapshotOptions tracks the items updated inside the lookback window.
var itemChangedSnapshotOptions = pollingtrigger.KeyedSnapshotOptions{
	MaxEntries: pollingtrigger.DefaultKeyedSnapshotEntries, KeepValues: true, EvictLeastRecentlySeen: true,
}

var errItemScanExceeded = fmt.Errorf("more than %d items changed since the last poll", maximumItemScanPages*itemPollPageSize)

// itemChangeFetcher reads one board's items newest-first by monday.com's last-updated time.
type itemChangeFetcher struct {
	client  *Client
	call    sdkgo.Call
	boardID string
	// pending holds the changes a scan found beyond the delivered batch, for the immediate next fetch.
	pending *pendingItemChanges
}

// itemChangeCursor is the committed position: the latest delivered last-updated time on one board. A zero
// Watermark means the board was empty at the baseline, so every item that appears is delivered.
type itemChangeCursor struct {
	BoardID   string    `json:"boardId"`
	Watermark time.Time `json:"watermark"`
}

type pendingItemChanges struct {
	cursorToken string
	changes     []itemChange
}

type itemChange struct {
	polled polledItem
	kind   ItemChangeKind
}

func newItemChangeFetcher(client *Client, connection sdkgo.ConnectionRef, configuration ItemChangedTriggerConfiguration) *itemChangeFetcher {
	return &itemChangeFetcher{
		client:  client,
		call:    pollingtrigger.NewTriggerCredentialCall(connection, ItemChangedTriggerDefinition.Trigger),
		boardID: configuration.BoardID,
	}
}

// SeedCursor records the newest last-updated time as the baseline and snapshots the items inside the
// lookback window before it, so no existing item is delivered.
func (fetcher *itemChangeFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	fetcher.pending = nil
	items, newest, err := fetcher.scanItemsUpdatedSince(ctx, time.Time{}, true)
	// An oversized baseline window keeps the items read so far; the rest may arrive once as changes.
	if err != nil && !errors.Is(err, errItemScanExceeded) {
		return pollingtrigger.Cursor{}, err
	}
	snapshot, err := pollingtrigger.ApplyKeyedSnapshotChanges(nil, keyedItems(items), nil, itemChangedSnapshotOptions)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	return fetcher.encodeCursor(itemChangeCursor{BoardID: fetcher.boardID, Watermark: newest}, snapshot)
}

// FetchChanges delivers, oldest first, the items whose content changed since the cursor. An undecodable
// cursor, a cursor for another board, or more changes than one scan can read marks the cursor invalid.
func (fetcher *itemChangeFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[ItemChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[ItemChangedEvent]]
	var position itemChangeCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("monday.com itemChanged cursor is not decodable"))
	}
	if position.BoardID != fetcher.boardID {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("monday.com itemChanged cursor belongs to another board"))
	}
	changes, err := fetcher.changesAfter(ctx, cursor, position)
	if err != nil {
		return page, err
	}
	batchSize := itemChangeBatchSize(changes)
	batch, rest := changes[:batchSize], changes[batchSize:]
	next := position
	for _, change := range batch {
		page.Changes = append(page.Changes, newItemChangedTriggerEvent(fetcher.boardID, change.polled, change.kind))
		if change.polled.item.UpdatedAt.After(next.Watermark) {
			next.Watermark = change.polled.item.UpdatedAt
		}
	}
	snapshot, err := nextItemSnapshot(cursor.Snapshot, batch, next.Watermark.Add(-itemChangeLookback))
	if err != nil {
		return page, err
	}
	if page.Next, err = fetcher.encodeCursor(next, snapshot); err != nil {
		return page, err
	}
	fetcher.pending = nil
	if len(rest) != 0 {
		fetcher.pending, page.HasMorePages = &pendingItemChanges{cursorToken: page.Next.Token, changes: rest}, true
	}
	return page, nil
}

// itemChangeBatchSize counts the oldest changes that fit one batch; the first change always fits.
func itemChangeBatchSize(changes []itemChange) int {
	encodedBytes := 0
	for index, change := range changes {
		encodedBytes += change.polled.encodedBytes
		if index == maximumItemChangesPerPage || (index > 0 && encodedBytes > maximumItemChangeBatchBytes) {
			return index
		}
	}
	return len(changes)
}

// changesAfter returns the changes a previous scan left for this exact cursor, or scans again.
func (fetcher *itemChangeFetcher) changesAfter(ctx context.Context, cursor pollingtrigger.Cursor, position itemChangeCursor) ([]itemChange, error) {
	if pending := fetcher.pending; pending != nil && pending.cursorToken == cursor.Token {
		return pending.changes, nil
	}
	fetcher.pending = nil
	floor := position.Watermark.Add(-itemChangeLookback)
	items, _, err := fetcher.scanItemsUpdatedSince(ctx, floor, false)
	if errors.Is(err, errItemScanExceeded) {
		return nil, pollingtrigger.MarkCursorInvalid(err)
	}
	if err != nil {
		return nil, err
	}
	previousHashes := map[string]string{}
	if cursor.Snapshot != nil {
		for _, entry := range cursor.Snapshot.Entries {
			previousHashes[entry.Key] = entry.Hash
		}
	}
	changes := make([]itemChange, 0, len(items))
	for _, polled := range items {
		previousHash, isKnown := previousHashes[polled.item.ID]
		if isKnown && previousHash == polled.hash {
			continue
		}
		kind := ItemChangeUpdated
		if !isKnown && !polled.item.CreatedAt.IsZero() && !polled.item.CreatedAt.Before(floor) {
			kind = ItemChangeCreated
		}
		changes = append(changes, itemChange{polled: polled, kind: kind})
	}
	slices.SortStableFunc(changes, func(left, right itemChange) int {
		if order := left.polled.item.UpdatedAt.Compare(right.polled.item.UpdatedAt); order != 0 {
			return order
		}
		return cmp.Compare(left.polled.item.ID, right.polled.item.ID)
	})
	return changes, nil
}

// scanItemsUpdatedSince reads pages ordered by last-updated time, newest first, until an item older than
// floor or the last page. With isFloorFromNewest it sets floor to the newest item's time minus the
// lookback. It returns the distinct items at or after floor and the newest last-updated time.
func (fetcher *itemChangeFetcher) scanItemsUpdatedSince(ctx context.Context, floor time.Time, isFloorFromNewest bool) ([]polledItem, time.Time, error) {
	request, _, err := buildListItemsRequest(ListItemsInput{
		BoardID: fetcher.boardID, Limit: itemPollPageSize, Order: &ItemOrder{ColumnID: ItemOrderLastUpdated, IsDescending: true},
	})
	if err != nil {
		return nil, time.Time{}, err
	}
	var items []polledItem
	var newest time.Time
	seenItemIDs := map[string]bool{}
	for pageIndex := 0; pageIndex < maximumItemScanPages; pageIndex++ {
		listed, err := fetcher.readItemsPage(ctx, request, pageIndex > 0)
		if err != nil {
			return nil, time.Time{}, err
		}
		hasReachedFloor := false
		for _, item := range listed.Items {
			if item.UpdatedAt.IsZero() || seenItemIDs[item.ID] {
				continue
			}
			seenItemIDs[item.ID] = true
			if newest.IsZero() && isFloorFromNewest {
				floor = item.UpdatedAt.Add(-itemChangeLookback)
			}
			if item.UpdatedAt.After(newest) {
				newest = item.UpdatedAt
			}
			if item.UpdatedAt.Before(floor) {
				hasReachedFloor = true
				continue
			}
			polled, err := newPolledItem(item)
			if err != nil {
				return nil, time.Time{}, err
			}
			items = append(items, polled)
		}
		if hasReachedFloor || listed.NextCursor == "" {
			return items, newest, nil
		}
		request, _, err = buildListItemsRequest(ListItemsInput{BoardID: fetcher.boardID, Limit: itemPollPageSize, Cursor: listed.NextCursor})
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("monday.com itemChanged poll: %w", err)
		}
	}
	return items, newest, errItemScanExceeded
}

// readItemsPage sends one items_page or next_items_page request under the runner's limiter.
func (fetcher *itemChangeFetcher) readItemsPage(ctx context.Context, request graphQLRequest, isContinuation bool) (decodedItemsPage, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return decodedItemsPage{}, err
	}
	result := fetcher.exchange(ctx, request)
	if result.outcome != exchangeSucceeded {
		return decodedItemsPage{}, fetcher.pollError(result)
	}
	page, isBoardFound, err := decodeItemsPage(result.response.data, isContinuation)
	switch {
	case err != nil:
		return decodedItemsPage{}, fmt.Errorf("monday.com itemChanged poll returned an invalid page of items: %w", err)
	case !isBoardFound:
		return decodedItemsPage{}, errors.New("monday.com itemChanged poll found no board with this ID that the connection can see")
	}
	return page, nil
}

// exchange resolves the connection's credential with the poller's context, sends the request, and after
// a rejected OAuth token refreshes once and resends.
func (fetcher *itemChangeFetcher) exchange(ctx context.Context, request graphQLRequest) mondayExchange {
	client := fetcher.client
	credentials, err := sdkgo.ResolveCredential(ctx, client.credentials, fetcher.call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return mondayExchange{outcome: exchangeDefect, failure: mondayFailure(sdkgo.FailureAuthentication, itemChangedOperationID, "monday.com connection credentials are unavailable")}
	}
	result := client.send(ctx, fetcher.call, credentials, itemChangedOperationID, request)
	if result.response.statusCode == http.StatusUnauthorized && client.canRefreshAfterRejection(credentials) {
		replacement, err := sdkgo.ResolveCredentialAfterRejection(ctx, client.credentials, fetcher.call, client.refreshDriver)
		if err == nil && validateResolvedCredentials(replacement) == nil {
			result = client.send(ctx, fetcher.call, replacement, itemChangedOperationID, request)
		}
	}
	return result
}

// pollError pauses the connection for a rate limit or an exhausted daily call limit; other failures
// back off this binding only.
func (fetcher *itemChangeFetcher) pollError(result mondayExchange) error {
	failure := fmt.Errorf("monday.com itemChanged poll: %s", result.failure.Message)
	switch result.failure.Kind {
	case sdkgo.FailureRateLimit:
		return pollingtrigger.MarkRateLimited(failure, result.retryAfter)
	case sdkgo.FailureQuotaExhausted:
		now := fetcher.client.now().UTC()
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		return pollingtrigger.MarkRateLimited(failure, max(result.retryAfter, nextMidnight.Sub(now)))
	}
	return failure
}

func (fetcher *itemChangeFetcher) encodeCursor(position itemChangeCursor, snapshot *projectconfig.KeyedSnapshot) (pollingtrigger.Cursor, error) {
	position.Watermark = position.Watermark.UTC()
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode monday.com itemChanged cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded), Snapshot: snapshot}, nil
}

// nextItemSnapshot adds the delivered items and drops entries updated before floor, which no later scan reads.
func nextItemSnapshot(previous *projectconfig.KeyedSnapshot, delivered []itemChange, floor time.Time) (*projectconfig.KeyedSnapshot, error) {
	upserted := make([]pollingtrigger.KeyedItem, 0, len(delivered))
	for _, change := range delivered {
		upserted = append(upserted, change.polled)
	}
	snapshot, err := pollingtrigger.ApplyKeyedSnapshotChanges(previous, upserted, nil, itemChangedSnapshotOptions)
	if err != nil {
		return nil, err
	}
	var expiredKeys []string
	for _, entry := range snapshot.Entries {
		var value snapshotValue
		if json.Unmarshal(entry.Value, &value) != nil || value.UpdatedAt.Before(floor) {
			expiredKeys = append(expiredKeys, entry.Key)
		}
	}
	if len(expiredKeys) == 0 {
		return snapshot, nil
	}
	return pollingtrigger.ApplyKeyedSnapshotChanges(snapshot, nil, expiredKeys, itemChangedSnapshotOptions)
}

func keyedItems(items []polledItem) []pollingtrigger.KeyedItem {
	keyed := make([]pollingtrigger.KeyedItem, 0, len(items))
	for _, polled := range items {
		keyed = append(keyed, polled)
	}
	return keyed
}
