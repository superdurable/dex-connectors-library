// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { DefaultKeyedSnapshotEntries, applyKeyedSnapshotChanges, markCursorInvalid, newTriggerCredentialCall, type ChangePage, type Cursor, type KeyedItem, type KeyedSnapshotOptions } from '@superdurable/connectors-sdk/pollingtrigger';
import type { KeyedSnapshot, KeyedSnapshotEntry } from '@superdurable/connectors-sdk/projectconfig';
import type { ConfluenceClient } from './client.js';
import { GoInstant } from './go_time.js';
import { decodePageChangeCursor, decodeSeenPageVersion, encodePageChangeCursor, encodeSeenPageVersion, pageChangeEventId, seenPageHash, type PageChangeCursor, type SeenPageVersion } from './page_change_cursor.js';
import { SearchCursorRejectedError, searchPageChanges, startTriggerSession, type ChangedPage, type SearchPageResult } from './page_change_search.js';
import { PageChangeCreated, PageChangeUpdated, PageChangedTriggerDefinition, type PageChangedEvent, type PageChangedTriggerConfiguration } from './page_changed.js';

/** Each scan re-reads ten minutes of edits, covering clock skew and search index lag. */
export const pageChangeLookbackNanoseconds = 10n * 60n * 1_000_000_000n;
/** Bounds the baseline scan of the lookback window. */
export const maximumSeedSearchPages = 100;
/** One entry per recently changed page; eviction can only repeat a page version, never lose one. */
export const pageChangeSnapshotOptions: KeyedSnapshotOptions = { maxEntries: DefaultKeyedSnapshotEntries, keepValues: true, evictLeastRecentlySeen: true };
const timeZoneMarginNanoseconds = 12n * 3600n * 1_000_000_000n;

/** One page version adapted to the kit's keyed snapshot. */
class SeenPageItem implements KeyedItem {
  constructor(private readonly pageId: string, private readonly seen: SeenPageVersion) {}
  snapshotKey(): string { return this.pageId; }
  snapshotHash(): string { return seenPageHash(this.pageId, this.seen.versionNumber); }
  snapshotValue(): Uint8Array { return encodeSeenPageVersion(this.seen); }
}

/** Bound the search by date, widened by half a day because CQL compares dates in the authorizing user's time zone. */
export function buildPageChangeCQL(spaceKey: string, windowStart: GoInstant): string {
  let clauses = 'type = page';
  if (spaceKey) clauses += ` AND space = "${spaceKey.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
  const lowerBound = windowStart.utc().addNanoseconds(-timeZoneMarginNanoseconds).formatCQLMinute();
  return `${clauses} AND lastmodified >= "${lowerBound}" ORDER BY lastmodified DESC`;
}

/** Convert one search result; its ID is a pure function of site, page, and version, so a refetch keeps it. */
export function newPageChangedTriggerEvent(cloudId: string, page: ChangedPage): TriggerEvent<PageChangedEvent> {
  const versionNumber = page.summary.versionNumber ?? 0;
  return {
    id: pageChangeEventId(cloudId, page.summary.id, versionNumber),
    occurredAt: page.lastModifiedAt.toDate(),
    payload: { cloudId, changeKind: versionNumber === 1 ? PageChangeCreated : PageChangeUpdated, page: page.summary },
  };
}

/** Scans Confluence CQL search, newest modification first, for one pageChanged binding. A poller owns it. */
export class PageChangeFetcher {
  private readonly spaceKey: string;

  /** Bind the client, connection, and watched space; validate the configuration beforehand. */
  constructor(private readonly client: ConfluenceClient, private readonly connection: ConnectionRef, configuration: PageChangedTriggerConfiguration) {
    this.spaceKey = configuration.spaceKey ?? '';
  }

  /** Record every page modified in the lookback window as seen, so the first scan delivers no existing page. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const session = await startTriggerSession(this.client, context, this.credentialCall(context));
    const startedAt = GoInstant.fromDate(this.client.now());
    const windowStart = startedAt.addNanoseconds(-pageChangeLookbackNanoseconds);
    const cql = buildPageChangeCQL(this.spaceKey, windowStart);
    const seen: KeyedItem[] = [];
    let searchCursor = '';
    for (let page = 0; page < maximumSeedSearchPages; page++) {
      const result = await searchPageChanges(this.client, context, session, cql, searchCursor);
      for (const changed of result.pages) if (!changed.lastModifiedAt.isBefore(windowStart)) seen.push(seenItem(changed));
      if (!result.searchCursor || hasPageModifiedBefore(result, windowStart)) {
        const snapshot = applyKeyedSnapshotChanges(undefined, seen, [], pageChangeSnapshotOptions);
        return encodeCursor({ cloudId: session.cloudId, spaceKey: this.spaceKey, scannedThrough: startedAt, scan: undefined }, snapshot);
      }
      searchCursor = result.searchCursor;
    }
    throw new Error(`Confluence pageChanged baseline exceeded ${maximumSeedSearchPages} search pages`);
  }

  /** Read one search page of the current scan; a cursor for another site or space, or an undecodable one, reseeds. */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<PageChangedEvent>>> {
    let position: PageChangeCursor;
    try { position = decodePageChangeCursor(cursor.token); }
    catch { position = { cloudId: '', spaceKey: '', scannedThrough: GoInstant.zero, scan: undefined }; }
    if (!position.cloudId || position.scannedThrough.isZero()) throw markCursorInvalid(new Error('Confluence pageChanged cursor is not decodable'));
    if (position.spaceKey !== this.spaceKey) throw markCursorInvalid(new Error('Confluence pageChanged cursor belongs to another space'));
    const session = await startTriggerSession(this.client, context, this.credentialCall(context));
    if (position.cloudId !== session.cloudId) throw markCursorInvalid(new Error('Confluence pageChanged cursor belongs to another site'));
    const windowStart = position.scannedThrough.addNanoseconds(-pageChangeLookbackNanoseconds);
    position.scan ??= { startedAt: GoInstant.fromDate(this.client.now()), cql: buildPageChangeCQL(this.spaceKey, windowStart), searchCursor: '' };
    let result: SearchPageResult;
    try { result = await searchPageChanges(this.client, context, session, position.scan.cql, position.scan.searchCursor); }
    catch (error) {
      if (!(error instanceof SearchCursorRejectedError)) throw error;
      // Restart this scan from its first page; the window is unchanged, so nothing is skipped.
      position.scan.searchCursor = '';
      return { changes: [], next: encodeCursor(position, cursor.snapshot), hasMorePages: true };
    }
    return collectPageChanges(session.cloudId, position, cursor.snapshot, result, windowStart);
  }

  private credentialCall(context: TriggerContext) { return newTriggerCredentialCall(this.connection, PageChangedTriggerDefinition.trigger, context.signal); }
}

/** Emit each unseen page version in the window, oldest first; the scan ends at the window's start or the last page. */
function collectPageChanges(cloudId: string, position: PageChangeCursor, snapshot: KeyedSnapshot | undefined, result: SearchPageResult, windowStart: GoInstant): ChangePage<TriggerEvent<PageChangedEvent>> {
  const stored = new Map((snapshot?.entries ?? []).map(entry => [entry.key, entry]));
  const changes: TriggerEvent<PageChangedEvent>[] = [];
  const upserted: KeyedItem[] = [];
  for (const page of result.pages) {
    if (page.lastModifiedAt.isBefore(windowStart) || isSeenPageVersion(stored.get(page.summary.id), page)) continue;
    changes.push(newPageChangedTriggerEvent(cloudId, page));
    upserted.push(seenItem(page));
  }
  changes.reverse();
  const scan = position.scan!;
  let removedKeys: string[] = [], hasMorePages = false;
  if (!result.searchCursor || hasPageModifiedBefore(result, windowStart)) {
    position.scannedThrough = scan.startedAt;
    position.scan = undefined;
    removedKeys = expiredSeenPageKeys(snapshot, position.scannedThrough.addNanoseconds(-pageChangeLookbackNanoseconds));
  } else {
    scan.searchCursor = result.searchCursor;
    hasMorePages = true;
  }
  const next = applyKeyedSnapshotChanges(snapshot, upserted, removedKeys, pageChangeSnapshotOptions);
  return { changes, next: encodeCursor(position, next), hasMorePages };
}

function encodeCursor(position: PageChangeCursor, snapshot: KeyedSnapshot | undefined): Cursor {
  return { token: encodePageChangeCursor(position), snapshot: snapshot ?? { entries: [] } };
}

function seenItem(page: ChangedPage): SeenPageItem {
  return new SeenPageItem(page.summary.id, { versionNumber: page.summary.versionNumber ?? 0, lastModifiedAt: page.lastModifiedAt.utc() });
}

function hasPageModifiedBefore(result: SearchPageResult, windowStart: GoInstant): boolean {
  return result.pages.some(page => page.lastModifiedAt.isBefore(windowStart));
}

/** Also skip an older version a lagging search index still returns; an unreadable entry counts as unseen. */
function isSeenPageVersion(entry: KeyedSnapshotEntry | undefined, page: ChangedPage): boolean {
  if (!entry) return false;
  try { return decodeSeenPageVersion(entry.value).versionNumber >= (page.summary.versionNumber ?? 0); }
  catch { return false; }
}

/** List entries no later scan can return again unless the page changes. */
function expiredSeenPageKeys(snapshot: KeyedSnapshot | undefined, nextWindowStart: GoInstant): string[] {
  return (snapshot?.entries ?? []).filter(entry => {
    try { return decodeSeenPageVersion(entry.value).lastModifiedAt.isBefore(nextWindowStart); }
    catch { return true; }
  }).map(entry => entry.key);
}
