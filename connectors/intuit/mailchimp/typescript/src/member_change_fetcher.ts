// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { TriggerDeliveryAbortedError, type ConnectionRef, type TriggerContext, type TriggerEvent, type TriggerRef } from '@superdurable/connectors-sdk';
import {
  applyKeyedSnapshotChanges, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest,
  type ChangeFetcher, type ChangePage, type Cursor, type KeyedItem, type KeyedSnapshotOptions,
} from '@superdurable/connectors-sdk/pollingtrigger';
import type { KeyedSnapshot } from '@superdurable/connectors-sdk/projectconfig';
import type { MailchimpClient } from './client.js';
import type { Member } from './generated/models.js';
import { MemberSnapshotItem, acceptsMemberStatus, newMemberChangedTriggerEvent, type MemberChangedEvent, type MemberChangedTriggerConfiguration } from './member_snapshot.js';
import { decodeMemberObject, memberFields, parseObject } from './operations.js';

/** Stays below the kit's 500 changes per page and Mailchimp's 1000-contact count. */
export const MemberPollPageSize = 250;
/** Rereads recent changes, because a change can become listable after a later one. */
const memberPollLookbackNs = 5n * 60n * 1_000_000_000n;
/** Bounds tie paging of contacts that share one change second. */
const maximumMemberPollOffset = 1_000_000;
const secondNs = 1_000_000_000n;
/** Keeps the most recently changed contacts; older ones are outside the lookback. */
export const MemberSnapshotOptions: KeyedSnapshotOptions = { evictLeastRecentlySeen: true };
const operation = 'memberChanged';

/** The committed token; times are RFC 3339 seconds in UTC, as Go's memberPollCursor encodes them. */
interface MemberPollCursor {
  audienceId: string;
  /** The newest change second at seed time; contacts changed before it are never delivered. */ baseline: string;
  /** The newest change time any pass has read. */ watermark: string;
  /** passSince and passOffset continue a pass that spans pages; both are empty between passes. */ passSince: string;
  passOffset: number;
}

interface MemberPage { members: Member[]; totalItems: number }

/** Lists one audience's contacts by last change time; the snapshot drops versions already delivered. */
export class MemberChangeFetcher implements ChangeFetcher<TriggerEvent<MemberChangedEvent>> {
  /** Bind the client, the connection and Trigger the credential call names, and the binding configuration. */
  constructor(private readonly client: MailchimpClient, private readonly connection: ConnectionRef, private readonly trigger: TriggerRef,
    private readonly configuration: MemberChangedTriggerConfiguration) {}

  /**
   * Record the newest change second as the baseline and snapshot the contacts changed in it, so a contact
   * changed later in that second is still delivered. An empty audience has no baseline. When a whole page
   * shares the newest second, the baseline moves one second later and none of that second is delivered.
   */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const page = await this.listMembers(context, new URLSearchParams({ count: String(MemberPollPageSize), offset: '0', sort_field: 'last_changed', sort_dir: 'DESC', fields: memberFields }));
    const audienceId = this.configuration.audienceId;
    const cursor: MemberPollCursor = { audienceId, baseline: '', watermark: '', passSince: '', passOffset: 0 };
    let newest: bigint | undefined;
    let seeded: KeyedItem[] = [];
    for (const member of page.members) {
      if (!member.lastChangedAt) continue;
      const changedAt = instantNs(member.lastChangedAt);
      newest ??= changedAt;
      if (changedAt !== newest) break;
      seeded.push(new MemberSnapshotItem(audienceId, member));
    }
    // No snapshot, as Go commits it: the stored cursor document then holds "snapshot":null.
    if (newest === undefined) return { token: encodeCursor(cursor) };
    cursor.baseline = cursor.watermark = formatCursorTime(newest);
    if (seeded.length === page.members.length && page.members.length === MemberPollPageSize) {
      // More contacts may share the newest second than one page holds, so none of it is delivered.
      cursor.baseline = formatCursorTime(newest + secondNs);
      seeded = [];
    }
    return { token: encodeCursor(cursor), snapshot: applyKeyedSnapshotChanges(undefined, seeded, [], MemberSnapshotOptions) };
  }

  /** Read one ascending page of changed contacts after the cursor's pass start. */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<MemberChangedEvent>>> {
    let position: MemberPollCursor;
    let since: bigint | undefined;
    try {
      position = this.decodeCursor(cursor.token);
      since = passStart(position);
    } catch (error) { throw markCursorInvalid(error); }
    const query = new URLSearchParams({ count: String(MemberPollPageSize), offset: String(position.passOffset), sort_field: 'last_changed', sort_dir: 'ASC', fields: memberFields });
    if (since !== undefined) query.set('since_last_changed', `${formatCursorTime(since).slice(0, 19)}+00:00`);
    const page = await this.listMembers(context, query);
    return this.buildChangePage(position, since, page, cursor.snapshot);
  }

  private buildChangePage(position: MemberPollCursor, since: bigint | undefined, page: MemberPage, snapshot: KeyedSnapshot | undefined): ChangePage<TriggerEvent<MemberChangedEvent>> {
    const audienceId = this.configuration.audienceId;
    const baseline = parseCursorTime(position.baseline);
    let watermark = parseCursorTime(position.watermark);
    const stored = new Map((snapshot?.entries ?? []).map(entry => [entry.key, entry.hash]));
    const changes: Array<TriggerEvent<MemberChangedEvent>> = [];
    const upserted: KeyedItem[] = [];
    const listed: Member[] = [];
    for (const member of page.members) {
      // Without a change time a contact cannot be ordered; Mailchimp sets it on every write.
      if (!member.lastChangedAt) continue;
      const changedAt = instantNs(member.lastChangedAt);
      listed.push(member);
      if (watermark === undefined || changedAt > watermark) watermark = changedAt;
      const item = new MemberSnapshotItem(audienceId, member);
      if (baseline !== undefined && changedAt < baseline) continue;
      if (stored.get(item.snapshotKey()) === item.snapshotHash()) continue;
      upserted.push(item);
      if (acceptsMemberStatus(this.configuration, member.status)) changes.push(newMemberChangedTriggerEvent(audienceId, member));
    }
    const nextSnapshot = applyKeyedSnapshotChanges(snapshot, upserted, [], MemberSnapshotOptions);
    const next: MemberPollCursor = { audienceId: position.audienceId, baseline: position.baseline, watermark: watermark === undefined ? '' : formatCursorTime(watermark), passSince: '', passOffset: 0 };
    const hasMorePages = page.members.length === MemberPollPageSize && position.passOffset + page.members.length < page.totalItems;
    if (hasMorePages) [next.passSince, next.passOffset] = continuePass(since, position.passOffset, page.members.length, listed);
    return { changes, next: { token: encodeCursor(next), snapshot: nextSnapshot }, hasMorePages };
  }

  private decodeCursor(token: string): MemberPollCursor {
    let value: unknown;
    try { value = JSON.parse(token); } catch { throw new Error('Mailchimp memberChanged cursor is not valid JSON'); }
    if (value === null) value = {};
    if (typeof value !== 'object' || Array.isArray(value)) throw new Error('Mailchimp memberChanged cursor is not valid JSON');
    const raw = value as Record<string, unknown>;
    const texts = ['audienceId', 'baseline', 'watermark', 'passSince'].map(name => {
      const text = raw[name] ?? '';
      if (typeof text !== 'string') throw new Error('Mailchimp memberChanged cursor is not valid JSON');
      return text;
    });
    const offset = raw.passOffset ?? 0;
    if (typeof offset !== 'number' || !Number.isSafeInteger(offset)) throw new Error('Mailchimp memberChanged cursor is not valid JSON');
    const position: MemberPollCursor = { audienceId: texts[0]!, baseline: texts[1]!, watermark: texts[2]!, passSince: texts[3]!, passOffset: offset };
    if (position.audienceId !== this.configuration.audienceId) throw new Error('Mailchimp memberChanged cursor belongs to another audience');
    for (const text of [position.baseline, position.watermark, position.passSince]) parseCursorTime(text);
    if (position.passOffset < 0 || position.passOffset > maximumMemberPollOffset) throw new Error('Mailchimp memberChanged cursor offset is out of range');
    return position;
  }

  /** Send one GET /lists/{list_id}/members and classify its failure for the kit. */
  private async listMembers(context: TriggerContext, query: URLSearchParams): Promise<MemberPage> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, this.trigger, context.signal);
    const connection = await this.client.resolveConnection(call);
    if (!connection) throw new Error('connection credentials are unavailable');
    const exchange = await this.client.exchange(call, connection, operation, 'GET', `/lists/${this.configuration.audienceId}/members`, query);
    if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
    const message = exchange.failure?.message ?? 'Mailchimp request failed';
    if (exchange.outcome === 'succeeded') {
      try { return decodeMemberPage(exchange.body); }
      catch (error) { throw new Error(`Mailchimp returned an invalid page of contacts: ${error instanceof Error ? error.message : String(error)}`); }
    }
    if (exchange.outcome === 'rateLimited') throw markRateLimited(new Error(message), exchange.retryAfterMs);
    if (exchange.outcome === 'rejected' && exchange.status === 400) throw markCursorInvalid(new Error(message));
    throw new Error(message);
  }
}

function decodeMemberPage(body: Uint8Array): MemberPage {
  const page = parseObject(body);
  const wires = page.members ?? [];
  if (!Array.isArray(wires) || typeof page.total_items !== 'number' || !Number.isSafeInteger(page.total_items) || page.total_items < 0 || wires.length > MemberPollPageSize) {
    throw new Error('the page has no total_items or more contacts than requested');
  }
  return { members: wires.map(wire => decodeMemberObject(wire, '')), totalItems: page.total_items };
}

/** The pending pass's bound, or the lookback before the watermark, never earlier than baseline - 1s. */
function passStart(position: MemberPollCursor): bigint | undefined {
  if (position.passSince || position.passOffset) return parseCursorTime(position.passSince);
  const watermark = parseCursorTime(position.watermark);
  if (watermark === undefined) return undefined;
  let start = watermark - memberPollLookbackNs;
  const baseline = parseCursorTime(position.baseline);
  // One second before the baseline lists the baseline second whether since_last_changed is exclusive or not.
  if (baseline !== undefined && start < baseline - secondNs) start = baseline - secondNs;
  return start;
}

/** Restart one second before the page's last change, or advance by offset within one second. */
function continuePass(since: bigint | undefined, offset: number, pageLength: number, listed: Member[]): [string, number] {
  if (listed.length) {
    const last = instantNs(listed[listed.length - 1]!.lastChangedAt!);
    const first = instantNs(listed[0]!.lastChangedAt!);
    const restart = last - secondNs;
    if (first < last && (since === undefined || restart > since)) return [formatCursorTime(restart), 0];
  }
  if (offset + pageLength > maximumMemberPollOffset) throw new Error(`more than ${maximumMemberPollOffset} Mailchimp contacts changed within one second`);
  return [since === undefined ? '' : formatCursorTime(since), offset + pageLength];
}

function encodeCursor(position: MemberPollCursor): string {
  const fields = [`"audienceId":${JSON.stringify(position.audienceId)}`];
  for (const [name, value] of [['baseline', position.baseline], ['watermark', position.watermark], ['passSince', position.passSince]] as const) {
    if (value) fields.push(`"${name}":"${value}"`);
  }
  if (position.passOffset) fields.push(`"passOffset":${position.passOffset}`);
  return `{${fields.join(',')}}`;
}

/** Nanoseconds since the Unix epoch of a canonical UTC timestamp, such as 2026-11-01T09:00:02.25Z. */
function instantNs(timestamp: string): bigint {
  const match = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?Z$/.exec(timestamp);
  if (!match) throw new Error('a contact timestamp is not canonical UTC');
  const seconds = BigInt(Date.parse(`${match[1]}Z`) / 1000);
  return seconds * secondNs + BigInt((match[2] ?? '').padEnd(9, '0') || '0');
}

/** Format as Go's time.RFC3339 in UTC, which truncates fractional seconds. */
function formatCursorTime(instant: bigint): string {
  const seconds = instant >= 0n ? instant / secondNs : -((-instant + secondNs - 1n) / secondNs);
  return new Date(Number(seconds) * 1000).toISOString().slice(0, 19) + 'Z';
}

function parseCursorTime(value: string): bigint | undefined {
  if (!value) return undefined;
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  const parsed = match ? Date.parse(value) : Number.NaN;
  if (!match || Number.isNaN(parsed)) throw new Error('Mailchimp memberChanged cursor holds an invalid time');
  const fraction = BigInt((match[7] ?? '').slice(0, 9).padEnd(9, '0') || '0');
  return BigInt(Math.floor(parsed / 1000)) * secondNs + fraction;
}
