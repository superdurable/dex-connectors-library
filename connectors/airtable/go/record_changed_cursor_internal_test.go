// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/go/internal/airtablefake"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// drainIDs fetches until a page reports no more and delivers nothing, counting deliveries per record.
func drainIDs(t *testing.T, fetcher *recordChangedFetcher, cursor pollingtrigger.Cursor, maximumFetches int) (map[string]int, pollingtrigger.Cursor) {
	t.Helper()
	delivered := map[string]int{}
	for range maximumFetches {
		ids, next, hasMore := fetchIDs(t, fetcher, cursor)
		require.LessOrEqual(t, len(next.Token), projectconfig.MaxPollCursorTokenBytes)
		cursor = next
		for _, id := range ids {
			delivered[id]++
		}
		if !hasMore && len(ids) == 0 {
			return delivered, cursor
		}
	}
	t.Fatalf("the feed did not become current within %d fetches", maximumFetches)
	return nil, cursor
}

func burstRecords(prefix string, count int, at time.Time) []airtablefake.Record {
	records := make([]airtablefake.Record, 0, count)
	for index := range count {
		records = append(records, fakeRecord(fmt.Sprintf("rec%s%0*d", prefix, 14-len(prefix), index), at))
	}
	return records
}

func TestABurstLargerThanTheCursorBoundIsDeliveredWithoutLossOrRepeats(t *testing.T) {
	table := airtablefake.New(t)
	fetcher := newFakeFetcher(t, table, 100)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	burstAt := seedTime.Add(time.Minute)
	table.Put(burstRecords("Burst", 2600, burstAt)...)

	delivered, cursor := drainIDs(t, fetcher, cursor, 200)
	require.Len(t, delivered, 2600, "every record of a 2,600-record bulk edit at one millisecond is delivered")
	for id, count := range delivered {
		require.Equal(t, 1, count, id)
	}

	table.Put(fakeRecord("recAfterBurst0001", burstAt.Add(time.Second)))
	delivered, _ = drainIDs(t, fetcher, cursor, 50)
	require.Equal(t, map[string]int{"recAfterBurst0001": 1}, delivered, "the poll is not stuck behind the delivered burst")
}

func TestRunsOfDeliveredRecordsLongerThanOneFetchResumeFromTheStoredOffset(t *testing.T) {
	table := airtablefake.New(t)
	fetcher := newFakeFetcher(t, table, 10)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	burstAt := seedTime.Add(time.Minute)
	table.Put(burstRecords("Run", 250, burstAt)...)
	delivered, cursor := drainIDs(t, fetcher, cursor, 200)
	require.Len(t, delivered, 250)

	// The next poll rescans the overlap: 250 remembered records, 25 pages, more than one fetch's 20.
	table.Put(fakeRecord("recAfterRun000001", burstAt.Add(2*time.Second)))
	delivered, _ = drainIDs(t, fetcher, cursor, 20)
	require.Equal(t, map[string]int{"recAfterRun000001": 1}, delivered)
}

func TestAChangeThatBecomesVisibleLateInsideTheOverlapIsDelivered(t *testing.T) {
	table := airtablefake.New(t)
	fetcher := newFakeFetcher(t, table, 100)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	table.Put(fakeRecord("recLaterStamp0001", seedTime.Add(4*time.Second)))
	delivered, cursor := drainIDs(t, fetcher, cursor, 10)
	require.Equal(t, map[string]int{"recLaterStamp0001": 1}, delivered)

	// A write stamped earlier that the API shows only now, within recordChangedOverlap.
	table.Put(fakeRecord("recEarlierStamp01", seedTime.Add(500*time.Millisecond)))
	delivered, cursor = drainIDs(t, fetcher, cursor, 10)
	require.Equal(t, map[string]int{"recEarlierStamp01": 1}, delivered)
	delivered, _ = drainIDs(t, fetcher, cursor, 10)
	require.Empty(t, delivered, "remembered records inside the overlap are not delivered again")
}

func TestAnExpiredOffsetRestartsTheScanWithoutLossOrRepeats(t *testing.T) {
	table := airtablefake.New(t)
	fetcher := newFakeFetcher(t, table, 10)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	table.Put(burstRecords("Expire", 35, seedTime.Add(time.Minute))...)
	delivered := map[string]int{}
	for range 30 {
		table.ExpireIterators()
		ids, next, hasMore := fetchIDs(t, fetcher, cursor)
		cursor = next
		for _, id := range ids {
			delivered[id]++
		}
		if !hasMore && len(ids) == 0 {
			break
		}
	}
	require.Len(t, delivered, 35)
	for id, count := range delivered {
		require.Equal(t, 1, count, id)
	}
}

func TestSeedHidesExistingRecordsBeyondItsFirstPage(t *testing.T) {
	table := airtablefake.New(t, burstRecords("Existing", 250, seedTime)...)
	fetcher := newFakeFetcher(t, table, 100)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	table.Put(fakeRecord("recNewAfterSeed01", seedTime.Add(time.Second)))
	delivered, _ := drainIDs(t, fetcher, cursor, 20)
	require.Equal(t, map[string]int{"recNewAfterSeed01": 1}, delivered, "records present at seed never start Flows")
}
