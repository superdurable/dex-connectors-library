// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	databaseRowCursorVersion = 1
	// DatabaseRowLookback is how far before the newest last edited time each poll queries again, so a row
	// Notion indexes late, or stamps with a time rounded down, is still delivered once.
	DatabaseRowLookback = 2 * time.Minute
	// MaximumSeenDatabaseRows bounds the remembered row versions inside the lookback. Rows evicted past it
	// may be delivered again with the same Trigger event ID, which a Dex Flow target does not start twice.
	MaximumSeenDatabaseRows = 2000
	// maximumSeenRowBytes bounds the encoded seen groups, so the cursor stays under the kit's 64 KiB token
	// limit even when every remembered row has its own millisecond last edited time.
	maximumSeenRowBytes = 48 << 10
	// maximumSeedPages bounds the baseline read of the lookback window.
	maximumSeedPages = 20
)

// databaseRowFetcher polls one data source's rows by last edited time for the poll Trigger kit.
type databaseRowFetcher struct {
	client       *Client
	call         sdkgo.Call
	dataSourceID string
}

// databaseRowCursor is the committed position: the newest last edited time read, the row versions seen
// inside the lookback before it, and Notion's start cursor while one query sequence spans pages.
type databaseRowCursor struct {
	Version      int            `json:"v"`
	DataSourceID string         `json:"dataSourceId"`
	HighWater    time.Time      `json:"highWater"`
	QueryFrom    *time.Time     `json:"queryFrom,omitempty"`
	StartCursor  string         `json:"startCursor,omitempty"`
	Seen         []seenRowGroup `json:"seen,omitempty"`
}

// seenRowGroup lists the rows delivered or baselined at one last edited time, as short row-ID digests.
type seenRowGroup struct {
	LastEditedTime time.Time `json:"at"`
	RowKeys        []string  `json:"rows"`
}

func newDatabaseRowFetcher(client *Client, connection sdkgo.ConnectionRef, configuration DatabaseRowChangedTriggerConfiguration) *databaseRowFetcher {
	return &databaseRowFetcher{
		client:       client,
		call:         pollingtrigger.NewTriggerCredentialCall(connection, DatabaseRowChangedTriggerDefinition.Trigger),
		dataSourceID: configuration.watchedDataSourceID(),
	}
}

// SeedCursor reads the newest last edited time, then remembers every row edited inside the lookback
// before it, so no existing row is delivered. An empty data source starts at the Unix epoch rather than
// the local clock, so a host clock ahead of Notion's cannot hide the first rows.
func (fetcher *databaseRowFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	newest, err := fetcher.client.queryDatabaseRowsForTrigger(ctx, fetcher.call, fetcher.dataSourceID, databaseRowQuery{isNewestFirst: true, pageSize: 1})
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	position := databaseRowCursor{Version: databaseRowCursorVersion, DataSourceID: fetcher.dataSourceID, HighWater: emptyDataSourceHighWater}
	if len(newest.rows) > 0 {
		position.HighWater = newest.rows[0].lastEditedTime
	}
	seen := newSeenRows(nil)
	from := position.HighWater.Add(-DatabaseRowLookback)
	startCursor := ""
	for range maximumSeedPages {
		page, err := fetcher.client.queryDatabaseRowsForTrigger(ctx, fetcher.call, fetcher.dataSourceID, databaseRowQuery{
			editedOnOrAfter: &from, startCursor: startCursor, pageSize: fetcher.client.pollPageSize,
		})
		if err != nil {
			return pollingtrigger.Cursor{}, err
		}
		for _, row := range page.rows {
			seen.add(row.lastEditedTime, row.page.ID)
			position.HighWater = laterTime(position.HighWater, row.lastEditedTime)
		}
		if page.nextCursor == "" {
			break
		}
		startCursor = page.nextCursor
	}
	position.Seen = seen.groupsWithin(position.HighWater)
	return encodeDatabaseRowCursor(position)
}

// FetchChanges queries rows edited at or after the lookback before the high water, oldest first, and
// returns each row version not yet seen. An undecodable cursor, or one for another data source, is marked
// invalid so the kit records a new baseline; a start cursor Notion rejects restarts the query instead.
func (fetcher *databaseRowFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[DatabaseRowChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[DatabaseRowChangedEvent]]
	var position databaseRowCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil || position.Version != databaseRowCursorVersion || position.HighWater.IsZero() {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Notion databaseRowChanged cursor is not decodable"))
	}
	if position.DataSourceID != fetcher.dataSourceID {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Notion databaseRowChanged cursor belongs to another data source"))
	}
	from := position.HighWater.Add(-DatabaseRowLookback)
	if position.StartCursor != "" && position.QueryFrom != nil {
		from = *position.QueryFrom
	}
	queried, err := fetcher.client.queryDatabaseRowsForTrigger(ctx, fetcher.call, fetcher.dataSourceID, databaseRowQuery{
		editedOnOrAfter: &from, startCursor: position.StartCursor, pageSize: fetcher.client.pollPageSize,
	})
	if errors.Is(err, errStaleStartCursor) {
		position.StartCursor, position.QueryFrom = "", nil
		page.Next, err = encodeDatabaseRowCursor(position)
		page.HasMorePages = true
		return page, err
	}
	if err != nil {
		return page, err
	}
	seen := newSeenRows(position.Seen)
	for _, row := range queried.rows {
		if row.lastEditedTime.Before(from) || seen.contains(row.lastEditedTime, row.page.ID) {
			continue
		}
		seen.add(row.lastEditedTime, row.page.ID)
		position.HighWater = laterTime(position.HighWater, row.lastEditedTime)
		page.Changes = append(page.Changes, newDatabaseRowChangedTriggerEvent(fetcher.dataSourceID, row.page, row.lastEditedTime))
	}
	position.StartCursor, position.QueryFrom = "", nil
	if queried.nextCursor != "" {
		position.StartCursor, position.QueryFrom, page.HasMorePages = queried.nextCursor, &from, true
	}
	position.Seen = seen.groupsWithin(position.HighWater)
	page.Next, err = encodeDatabaseRowCursor(position)
	return page, err
}

// emptyDataSourceHighWater makes an empty data source's first query start at the Unix epoch.
var emptyDataSourceHighWater = time.Unix(0, 0).UTC().Add(DatabaseRowLookback)

func encodeDatabaseRowCursor(position databaseRowCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Notion databaseRowChanged cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}

func laterTime(current time.Time, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate
	}
	return current
}

// seenRows indexes row-ID digests by last edited time in UnixNano.
type seenRows map[int64]map[string]bool

func newSeenRows(groups []seenRowGroup) seenRows {
	seen := seenRows{}
	for _, group := range groups {
		for _, key := range group.RowKeys {
			seen.addKey(group.LastEditedTime, key)
		}
	}
	return seen
}

func (seen seenRows) contains(lastEditedTime time.Time, rowID string) bool {
	return seen[lastEditedTime.UnixNano()][seenRowKey(rowID)]
}

func (seen seenRows) add(lastEditedTime time.Time, rowID string) {
	seen.addKey(lastEditedTime, seenRowKey(rowID))
}

func (seen seenRows) addKey(lastEditedTime time.Time, key string) {
	instant := lastEditedTime.UnixNano()
	if seen[instant] == nil {
		seen[instant] = map[string]bool{}
	}
	seen[instant][key] = true
}

// groupsWithin keeps the newest row versions edited inside the lookback, at most MaximumSeenDatabaseRows
// and maximumSeenRowBytes once encoded.
func (seen seenRows) groupsWithin(highWater time.Time) []seenRowGroup {
	lowerBound := highWater.Add(-DatabaseRowLookback).UnixNano()
	instants := make([]int64, 0, len(seen))
	for instant := range seen {
		if instant >= lowerBound {
			instants = append(instants, instant)
		}
	}
	slices.Sort(instants)
	slices.Reverse(instants)
	var groups []seenRowGroup
	remaining, remainingBytes := MaximumSeenDatabaseRows, maximumSeenRowBytes
	for _, instant := range instants {
		lastEditedTime := time.Unix(0, instant).UTC()
		// A group encodes as {"at":"<RFC 3339>","rows":[...]}, and each key as "<16 characters>",.
		groupBytes := len(`{"at":"","rows":[]},`) + len(lastEditedTime.Format(time.RFC3339Nano))
		keyBytes := len(`"",`) + len(seenRowKey(""))
		fitting := min(remaining, (remainingBytes-groupBytes)/keyBytes)
		if fitting <= 0 {
			break
		}
		keys := make([]string, 0, len(seen[instant]))
		for key := range seen[instant] {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		keys = keys[:min(len(keys), fitting)]
		remaining -= len(keys)
		remainingBytes -= groupBytes + len(keys)*keyBytes
		groups = append(groups, seenRowGroup{LastEditedTime: lastEditedTime, RowKeys: keys})
	}
	slices.Reverse(groups)
	return groups
}

// seenRowKey is a 96-bit row-ID digest: unique enough within one data source's lookback, and short.
func seenRowKey(rowID string) string {
	digest := sha256.Sum256([]byte(rowID))
	return base64.RawURLEncoding.EncodeToString(digest[:12])
}
