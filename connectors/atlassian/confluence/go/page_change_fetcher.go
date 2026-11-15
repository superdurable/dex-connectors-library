// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const (
	// pageChangeLookback re-reads recent edits each scan, covering clock skew and search index lag.
	pageChangeLookback = 10 * time.Minute
	// pageChangeSearchPageSize asks for Confluence's largest content search page; it may return fewer.
	pageChangeSearchPageSize = maximumSearchPageSize
	// maximumSeedSearchPages bounds the baseline scan of the lookback window.
	maximumSeedSearchPages  = 100
	pageChangeCQLDateLayout = "2006-01-02 15:04"
)

// pageChangeSnapshotOptions keeps one entry per recently changed page. Eviction can only repeat a page
// version, never lose one. Each entry stores about 130 bytes with its value, so the kit's default bound
// keeps the cursor document near 1.3 MB; the 100,000-entry ceiling would outgrow project object storage
// before eviction started.
var pageChangeSnapshotOptions = pollingtrigger.KeyedSnapshotOptions{
	MaxEntries: pollingtrigger.DefaultKeyedSnapshotEntries, KeepValues: true, EvictLeastRecentlySeen: true,
}

// pageChangeFetcher scans Confluence CQL search, newest modification first, for one pageChanged binding.
type pageChangeFetcher struct {
	client   *Client
	call     sdkgo.Call
	spaceKey string
}

// pageChangeCursor is the committed Trigger cursor. ScannedThrough is when the last complete scan began;
// the next scan reads every page modified since ScannedThrough minus pageChangeLookback.
type pageChangeCursor struct {
	CloudID        string          `json:"cloudId"`
	SpaceKey       string          `json:"spaceKey,omitempty"`
	ScannedThrough time.Time       `json:"scannedThrough"`
	Scan           *pageChangeScan `json:"scan,omitempty"`
}

// pageChangeScan is a scan that spans several search pages. Confluence's search cursor continues only
// the exact query that produced it, so the CQL is pinned.
type pageChangeScan struct {
	StartedAt    time.Time `json:"startedAt"`
	CQL          string    `json:"cql"`
	SearchCursor string    `json:"searchCursor,omitempty"`
}

// seenPageVersion is the snapshot value of one delivered or baseline page version.
type seenPageVersion struct {
	VersionNumber  int       `json:"versionNumber"`
	LastModifiedAt time.Time `json:"lastModifiedAt"`
}

// seenPageItem adapts one page version to pollingtrigger.KeyedItem.
type seenPageItem struct {
	pageID string
	seen   seenPageVersion
}

// searchPageResult is one decoded search page.
type searchPageResult struct {
	pages        []PageSummary
	searchCursor string
}

func newPageChangeFetcher(client *Client, connection sdkgo.ConnectionRef, configuration PageChangedTriggerConfiguration) *pageChangeFetcher {
	return &pageChangeFetcher{
		client:   client,
		call:     pollingtrigger.NewTriggerCredentialCall(connection, PageChangedTriggerDefinition.Trigger),
		spaceKey: configuration.SpaceKey,
	}
}

// SeedCursor records every page modified within the lookback window as already seen, so the first scan
// delivers no page that existed before the Trigger started.
func (fetcher *pageChangeFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	session, cancel, err := fetcher.client.startTriggerSession(ctx, fetcher.call)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	defer cancel()
	startedAt := fetcher.client.now().UTC()
	cql := buildPageChangeCQL(fetcher.spaceKey, startedAt.Add(-pageChangeLookback))
	var seen []pollingtrigger.KeyedItem
	searchCursor := ""
	for page := 0; page < maximumSeedSearchPages; page++ {
		result, err := fetcher.client.searchPageChanges(session, cql, searchCursor)
		if err != nil {
			return pollingtrigger.Cursor{}, err
		}
		for _, summary := range result.pages {
			if !summary.LastModifiedAt.Before(startedAt.Add(-pageChangeLookback)) {
				seen = append(seen, newSeenPageItem(summary))
			}
		}
		if result.searchCursor == "" || hasPageModifiedBefore(result.pages, startedAt.Add(-pageChangeLookback)) {
			snapshot, err := pollingtrigger.ApplyKeyedSnapshotChanges(nil, seen, nil, pageChangeSnapshotOptions)
			if err != nil {
				return pollingtrigger.Cursor{}, err
			}
			return encodePageChangeCursor(pageChangeCursor{CloudID: session.cloudID, SpaceKey: fetcher.spaceKey, ScannedThrough: startedAt}, snapshot)
		}
		searchCursor = result.searchCursor
	}
	return pollingtrigger.Cursor{}, fmt.Errorf("Confluence pageChanged baseline exceeded %d search pages", maximumSeedSearchPages)
}

// FetchChanges reads one search page of the current scan. A cursor that cannot be decoded, or that
// belongs to another site or space, is marked invalid so the kit records a new baseline.
func (fetcher *pageChangeFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[PageChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[PageChangedEvent]]
	var position pageChangeCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil || position.CloudID == "" || position.ScannedThrough.IsZero() {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Confluence pageChanged cursor is not decodable"))
	}
	if position.SpaceKey != fetcher.spaceKey {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Confluence pageChanged cursor belongs to another space"))
	}
	session, cancel, err := fetcher.client.startTriggerSession(ctx, fetcher.call)
	if err != nil {
		return page, err
	}
	defer cancel()
	if position.CloudID != session.cloudID {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Confluence pageChanged cursor belongs to another site"))
	}
	windowStart := position.ScannedThrough.Add(-pageChangeLookback)
	if position.Scan == nil {
		position.Scan = &pageChangeScan{StartedAt: fetcher.client.now().UTC(), CQL: buildPageChangeCQL(fetcher.spaceKey, windowStart)}
	}
	result, err := fetcher.client.searchPageChanges(session, position.Scan.CQL, position.Scan.SearchCursor)
	if errors.Is(err, errSearchCursorRejected) {
		// Restart this scan from its first page; the window is unchanged, so nothing is skipped.
		position.Scan.SearchCursor = ""
		page.HasMorePages = true
		page.Next, err = encodePageChangeCursor(position, cursor.Snapshot)
		return page, err
	}
	if err != nil {
		return page, err
	}
	return fetcher.collectPageChanges(session.cloudID, position, cursor.Snapshot, result, windowStart)
}

// collectPageChanges emits each page version in the window that the snapshot has not seen, oldest first.
// The scan ends at the first page older than the window or at the last search page.
func (fetcher *pageChangeFetcher) collectPageChanges(cloudID string, position pageChangeCursor, snapshot *projectconfig.KeyedSnapshot, result searchPageResult, windowStart time.Time) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[PageChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[PageChangedEvent]]
	var upserted []pollingtrigger.KeyedItem
	for _, summary := range result.pages {
		if summary.LastModifiedAt.Before(windowStart) || isSeenPageVersion(snapshot, summary) {
			continue
		}
		page.Changes = append(page.Changes, newPageChangedTriggerEvent(cloudID, summary))
		upserted = append(upserted, newSeenPageItem(summary))
	}
	slices.Reverse(page.Changes)
	var removedKeys []string
	if result.searchCursor == "" || hasPageModifiedBefore(result.pages, windowStart) {
		position.ScannedThrough, position.Scan = position.Scan.StartedAt, nil
		removedKeys = expiredSeenPageKeys(snapshot, position.ScannedThrough.Add(-pageChangeLookback))
	} else {
		position.Scan.SearchCursor = result.searchCursor
		page.HasMorePages = true
	}
	next, err := pollingtrigger.ApplyKeyedSnapshotChanges(snapshot, upserted, removedKeys, pageChangeSnapshotOptions)
	if err != nil {
		return page, err
	}
	page.Next, err = encodePageChangeCursor(position, next)
	return page, err
}

// buildPageChangeCQL bounds the search by date. CQL compares dates in the authorizing user's time zone,
// so the bound is widened by half a day and the window is enforced on each page's absolute time.
func buildPageChangeCQL(spaceKey string, windowStart time.Time) string {
	clauses := "type = page"
	if spaceKey != "" {
		clauses += " AND space = " + MustQuoteCQLString(spaceKey)
	}
	lowerBound := windowStart.UTC().Add(-modifiedSinceTimeZoneMargin).Format(pageChangeCQLDateLayout)
	return clauses + ` AND lastmodified >= "` + lowerBound + `" ORDER BY lastmodified DESC`
}

func encodePageChangeCursor(position pageChangeCursor, snapshot *projectconfig.KeyedSnapshot) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Confluence pageChanged cursor: %w", err)
	}
	if snapshot == nil {
		snapshot = &projectconfig.KeyedSnapshot{}
	}
	return pollingtrigger.Cursor{Token: string(encoded), Snapshot: snapshot}, nil
}

// hasPageModifiedBefore reports that a newest-first search page reached pages older than windowStart.
func hasPageModifiedBefore(pages []PageSummary, windowStart time.Time) bool {
	return slices.ContainsFunc(pages, func(summary PageSummary) bool { return summary.LastModifiedAt.Before(windowStart) })
}

// isSeenPageVersion also skips an older version a lagging search index still returns.
func isSeenPageVersion(snapshot *projectconfig.KeyedSnapshot, summary PageSummary) bool {
	seen, ok := lookupSeenPageVersion(snapshot, summary.ID)
	return ok && seen.VersionNumber >= summary.VersionNumber
}

func lookupSeenPageVersion(snapshot *projectconfig.KeyedSnapshot, pageID string) (seenPageVersion, bool) {
	if snapshot == nil {
		return seenPageVersion{}, false
	}
	index, found := slices.BinarySearchFunc(snapshot.Entries, pageID, func(entry projectconfig.KeyedSnapshotEntry, key string) int {
		switch {
		case entry.Key < key:
			return -1
		case entry.Key > key:
			return 1
		}
		return 0
	})
	if !found {
		return seenPageVersion{}, false
	}
	var seen seenPageVersion
	if err := json.Unmarshal(snapshot.Entries[index].Value, &seen); err != nil {
		// An unreadable entry counts as unseen, which can repeat a delivery but never lose one.
		return seenPageVersion{}, false
	}
	return seen, true
}

// expiredSeenPageKeys lists entries no later scan can return again unless the page changes.
func expiredSeenPageKeys(snapshot *projectconfig.KeyedSnapshot, nextWindowStart time.Time) []string {
	if snapshot == nil {
		return nil
	}
	var keys []string
	for _, entry := range snapshot.Entries {
		var seen seenPageVersion
		if err := json.Unmarshal(entry.Value, &seen); err != nil || seen.LastModifiedAt.Before(nextWindowStart) {
			keys = append(keys, entry.Key)
		}
	}
	return keys
}

func newSeenPageItem(summary PageSummary) seenPageItem {
	return seenPageItem{pageID: summary.ID, seen: seenPageVersion{VersionNumber: summary.VersionNumber, LastModifiedAt: summary.LastModifiedAt.UTC()}}
}

// SnapshotKey returns the page ID.
func (item seenPageItem) SnapshotKey() string { return item.pageID }

// SnapshotHash hashes the page version.
func (item seenPageItem) SnapshotHash() string {
	digest := sha256.Sum256([]byte(item.pageID + "\x00" + strconv.Itoa(item.seen.VersionNumber)))
	return hex.EncodeToString(digest[:8])
}

// SnapshotValue returns the version number and modification time used for skipping and expiry.
func (item seenPageItem) SnapshotValue() json.RawMessage {
	encoded, err := json.Marshal(item.seen)
	if err != nil {
		panic(fmt.Errorf("encode Confluence seen page version: %w", err))
	}
	return encoded
}
