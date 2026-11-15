// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, TriggerDeliveryAbortedError, resolveCredential, type Call, type ConnectionRef, type TriggerContext, type TriggerEvent, type TriggerRef } from '@superdurable/connectors-sdk';
import {
  DefaultKeyedSnapshotEntries, applyKeyedSnapshotChanges, diffKeyedSnapshot, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest,
  type ChangeFetcher, type ChangePage, type Cursor, type KeyedSnapshotDiff, type KeyedSnapshotOptions,
} from '@superdurable/connectors-sdk/pollingtrigger';
import type { KeyedSnapshot, KeyedSnapshotEntry } from '@superdurable/connectors-sdk/projectconfig';
import { isHeaderSafeCredential } from '@superdurable/connectors-sdk/providerhttp';
import type { ZoomClient } from './client.js';
import type { Credentials, MeetingPage } from './generated/models.js';
import { decodeMeeting, integer, meetingSummary, pageToken, responseObject } from './meeting.js';
import {
  ListedMeeting, MeetingChangeRemoved, MeetingChangeScheduled, MeetingChangeUpdated, decodeTrackedMeeting, deliversMeetingChange, goJSON, newMeetingChangedTriggerEvent,
  type MeetingChangeKind, type MeetingChangedEvent, type MeetingChangedTriggerConfiguration,
} from './meeting_snapshot.js';
import { classify } from './operations.js';

/** Versions the committed cursor token; another value records a new baseline. */
const meetingListCursorVersion = 1;
const maximumPageSize = 300;
/** The snapshot bound at Zoom's largest page size, plus one page. */
const maximumMeetingListPages = Math.floor(DefaultKeyedSnapshotEntries / maximumPageSize) + 2;
/** Keeps one commit small; further changes follow in the same poll. */
export const MeetingChangesPerPage = 100;
/** Keeps each meeting's last listed state, so updates and removals carry it. */
export const MeetingSnapshotOptions: KeyedSnapshotOptions = { keepValues: true };

/** Zoom answered 400 to a later page, as it does after the 15-minute page token expiry. */
class PageTokenRejectedError extends Error {}

/** The committed token: hostId owns the snapshot, sequence counts the commits that carried a difference. */
interface MeetingListCursor { version: number; hostId: string; sequence: number }

function encodeMeetingListCursor(position: MeetingListCursor): string {
  const fields = [`"version":${position.version}`];
  if (position.hostId) fields.push(`"hostId":${goJSON(position.hostId)}`);
  if (position.sequence) fields.push(`"sequence":${position.sequence}`);
  return `{${fields.join(',')}}`;
}

/** Decode the token as Go's json.Unmarshal into meetingListCursor does, or undefined when Go would fail. */
function decodeMeetingListCursor(token: string): MeetingListCursor | undefined {
  let value: unknown;
  try { value = responseObjectOrNull(token); } catch { return undefined; }
  if (value === null) return { version: 0, hostId: '', sequence: 0 };
  const raw = value as Record<string, unknown>;
  try {
    const hostId = raw.hostId ?? '';
    if (typeof hostId !== 'string') return undefined;
    return { version: integer(raw.version), hostId, sequence: integer(raw.sequence) };
  } catch { return undefined; }
}

function responseObjectOrNull(token: string): Record<string, unknown> | null {
  if (token.trim() === 'null') return null;
  return responseObject(Buffer.from(token, 'utf8'));
}

/** One complete listing, one entry per meeting ID, sorted by key. */
interface MeetingListing { meetings: ListedMeeting[]; byKey: Map<string, ListedMeeting>; hostIds: Set<string> }

function soleHostId(listing: MeetingListing): string {
  return listing.hostIds.size === 1 ? [...listing.hostIds][0]! : '';
}

/** One difference between the stored snapshot and a listing. */
interface MeetingSnapshotChange { kind: MeetingChangeKind; key: string; previous?: KeyedSnapshotEntry; current?: ListedMeeting }

const compareKeys = (left: string, right: string) => Buffer.compare(Buffer.from(left), Buffer.from(right));

/** Diffs full meeting listings; Zoom has no change feed, so the snapshot is the cursor. */
export class MeetingListFetcher implements ChangeFetcher<TriggerEvent<MeetingChangedEvent>> {
  /** Bind the client, connection, Trigger identity, and validated configuration. */
  constructor(private readonly client: ZoomClient, private readonly connection: ConnectionRef, private readonly trigger: TriggerRef, private readonly configuration: MeetingChangedTriggerConfiguration) {}

  /** List every scheduled meeting and keep the listing as the baseline without delivering it. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const listing = await this.listAllMeetings(context);
    const diff = withMessagePrefix('Zoom meeting baseline', () => diffKeyedSnapshot(undefined, listing.meetings, MeetingSnapshotOptions));
    return { token: encodeMeetingListCursor({ version: meetingListCursorVersion, hostId: soleHostId(listing), sequence: 0 }), snapshot: diff.next };
  }

  /** Return up to MeetingChangesPerPage snapshot differences; a foreign or undecodable cursor reseeds. */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<MeetingChangedEvent>>> {
    const position = decodeMeetingListCursor(cursor.token);
    if (!position || position.version !== meetingListCursorVersion || !cursor.snapshot) throw markCursorInvalid(new Error('Zoom meeting list cursor is not decodable'));
    const listing = await this.listAllMeetings(context);
    if (position.hostId && listing.meetings.length > 0 && !listing.hostIds.has(position.hostId)) {
      throw markCursorInvalid(new Error('Zoom meeting list belongs to another Zoom user than the stored snapshot'));
    }
    if (!position.hostId) position.hostId = soleHostId(listing);
    const stored = cursor.snapshot;
    const diff = withMessagePrefix('Zoom meeting snapshot', () => diffKeyedSnapshot(stored, listing.meetings, MeetingSnapshotOptions));
    let changes = orderedMeetingChanges(diff, listing);
    let next = diff.next, hasMorePages = false;
    const sequence = position.sequence;
    if (changes.length > 0) position.sequence++;
    if (changes.length > MeetingChangesPerPage) {
      changes = changes.slice(0, MeetingChangesPerPage);
      hasMorePages = true;
      next = applyMeetingChanges(stored, changes);
    }
    const observedAt = this.client.currentTime();
    const events = changes.filter(change => deliversMeetingChange(this.configuration, change.kind)).map(change => changeTriggerEvent(change, sequence, observedAt));
    return { changes: events, next: { token: encodeMeetingListCursor(position), snapshot: next }, hasMorePages };
  }

  // Zoom expires page tokens after 15 minutes, so a 400 on a later page restarts the listing once.
  private async listAllMeetings(context: TriggerContext): Promise<MeetingListing> {
    try { return await this.doListAllMeetings(context); }
    catch (error) { if (!(error instanceof PageTokenRejectedError)) throw error; }
    try { return await this.doListAllMeetings(context); }
    catch (error) { if (error instanceof PageTokenRejectedError) throw new Error(error.message); throw error; }
  }

  private async doListAllMeetings(context: TriggerContext): Promise<MeetingListing> {
    const listing: MeetingListing = { meetings: [], byKey: new Map(), hostIds: new Set() };
    const requestedPageTokens = new Set<string>();
    let currentPageToken = '';
    for (let pageNumber = 0; pageNumber < maximumMeetingListPages; pageNumber++) {
      const listed = await this.listMeetingPage(context, currentPageToken);
      for (const summary of listed.meetings ?? []) {
        const meeting = new ListedMeeting(summary), key = meeting.snapshotKey();
        // Zoom can list a meeting twice, such as a recurring meeting's occurrences; the earliest start wins.
        const existing = listing.byKey.get(key);
        if (!existing || meeting.listsBefore(existing)) listing.byKey.set(key, meeting);
        if (summary.hostId) listing.hostIds.add(summary.hostId);
      }
      if (!listed.nextPageToken) {
        listing.meetings = [...listing.byKey.keys()].sort(compareKeys).map(key => listing.byKey.get(key)!);
        return listing;
      }
      if (requestedPageTokens.has(listed.nextPageToken)) throw new Error('Zoom meeting list repeated a page token');
      requestedPageTokens.add(listed.nextPageToken);
      currentPageToken = listed.nextPageToken;
    }
    throw new Error(`Zoom meeting list exceeded ${maximumMeetingListPages} pages`);
  }

  /** Send one List meetings request; a 400 to a page token rejects with PageTokenRejectedError. */
  private async listMeetingPage(context: TriggerContext, currentPageToken: string): Promise<MeetingPage> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, this.trigger, context.signal);
    const credentials = await this.resolveCredentials(call, context.signal);
    const query = new URLSearchParams({ type: 'scheduled', page_size: String(maximumPageSize) });
    if (currentPageToken) query.set('next_page_token', currentPageToken);
    const exchange = await this.client.exchange(call, credentials, 'GET', '/users/me/meetings', query, undefined, false, context.signal);
    if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
    if ('error' in exchange) throw new Error('Zoom meeting list request could not reach Zoom');
    if (currentPageToken && exchange.response.status === 400) throw new PageTokenRejectedError('Zoom rejected the meeting list page token with HTTP 400');
    const classified = classify(this.client, 'listMeetings', exchange);
    if (classified.outcome !== 'usable') {
      const failure = new Error(`Zoom meeting list request: ${classified.failure?.message ?? 'Zoom returned an unusable meeting page'}`);
      // A daily-limit 429 rejects a Step; a poller waits Retry-After, or backs off without it.
      if (exchange.response.status === 429) throw markRateLimited(failure, this.client.retryAfter(exchange.response.headers));
      throw failure;
    }
    try { return decodeMeetingPage(exchange.response.body); }
    catch { throw new Error('Zoom returned an unusable meeting page'); }
  }

  private async resolveCredentials(call: Call, signal: AbortSignal): Promise<Credentials> {
    let credentials: Credentials;
    try { credentials = await resolveCredential(this.client.credentials, call, this.client.refreshDriver, signal); }
    catch (error) {
      if (error instanceof ReauthorizationRequiredError) throw new Error('Zoom connection requires reauthorization; reconnect the connection');
      throw new Error('Zoom Trigger credentials are unavailable');
    }
    if (!credentials.accessToken || !isHeaderSafeCredential(credentials.accessToken.reveal())) throw new Error('Zoom Trigger credentials are unavailable');
    return credentials;
  }
}

/** Decode one complete meeting page as listMeetings does. */
function decodeMeetingPage(body: Uint8Array): MeetingPage {
  const raw = responseObject(body), total = integer(raw.total_records), next = raw.next_page_token ?? '';
  if (total < 0 || typeof next !== 'string' || (raw.meetings !== undefined && raw.meetings !== null && !Array.isArray(raw.meetings))) throw new TypeError('invalid page');
  pageToken(next);
  const meetings = ((raw.meetings ?? []) as unknown[]).map(item => meetingSummary(decodeMeeting(item)));
  return { meetings, totalRecords: total, ...(next ? { nextPageToken: next } : {}) };
}

function withMessagePrefix<Result>(prefix: string, compute: () => Result): Result {
  try { return compute(); }
  catch (error) { if (error instanceof Error) error.message = `${prefix}: ${error.message}`; throw error; }
}

/** Merge the diff's sorted lists into one list in meeting key order. */
function orderedMeetingChanges(diff: KeyedSnapshotDiff, listing: MeetingListing): MeetingSnapshotChange[] {
  const changes: MeetingSnapshotChange[] = [
    ...diff.added.map(added => ({ kind: MeetingChangeScheduled, key: added.key, current: listing.byKey.get(added.key)! }) as MeetingSnapshotChange),
    ...diff.changed.map(changed => ({ kind: MeetingChangeUpdated, key: changed.current.key, previous: changed.previous, current: listing.byKey.get(changed.current.key)! }) as MeetingSnapshotChange),
    ...diff.removed.map(removed => ({ kind: MeetingChangeRemoved, key: removed.key, previous: removed }) as MeetingSnapshotChange),
  ];
  // Array.prototype.sort is stable, as Go's sort.SliceStable.
  return changes.sort((left, right) => compareKeys(left.key, right.key));
}

/** Advance the stored snapshot by only the changes this page returns. */
function applyMeetingChanges(previous: KeyedSnapshot, changes: MeetingSnapshotChange[]): KeyedSnapshot {
  const upserted = changes.flatMap(change => change.current ? [change.current] : []);
  const removedKeys = changes.filter(change => change.kind === MeetingChangeRemoved).map(change => change.key);
  return withMessagePrefix('Zoom meeting snapshot', () => applyKeyedSnapshotChanges(previous, upserted, removedKeys, MeetingSnapshotOptions));
}

function changeTriggerEvent(change: MeetingSnapshotChange, sequence: number, observedAt: Date): TriggerEvent<MeetingChangedEvent> {
  const payload: MeetingChangedEvent = { changeKind: change.kind, meeting: change.current?.summary ?? { id: 0, topic: '' } };
  if (change.previous) {
    const previous = decodeTrackedMeeting(change.previous);
    if (change.kind === MeetingChangeRemoved) payload.meeting = previous;
    else payload.previousMeeting = previous;
  }
  return newMeetingChangedTriggerEvent(change.previous, change.current?.snapshotHash() ?? '', sequence, payload, observedAt);
}
