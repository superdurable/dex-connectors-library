// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import { decodeGoString, goFieldMembers, parseGoJSON } from './go_json.js';
import { GoTime, parseGoTime } from './go_time.js';

/** How far, in nanoseconds, before the committed high-water mark each poll starts reading again. */
export const TicketEventOverlapNanoseconds = 15_000_000_000n;
/** Bounds the cursor's dedupe set under the kit's 64 KiB cursor token limit. */
export const MaximumSeenEventKeys = 1500;

const int64Pattern = /^[+-]?[0-9]+$/;

/** Thrown for a ticketChanged cursor token that is not a cursor Go's json.Unmarshal and time.Parse accept. */
export class TicketEventCursorInvalidError extends Error {
  /** Describe the rejected token without echoing it. */
  constructor() { super('Gorgias ticketChanged cursor is not decodable'); this.name = 'TicketEventCursorInvalidError'; }
}

/** The committed position: the newest created_datetime read, a scan start, and recent event keys. */
export interface TicketEventCursor { since: string; scanFrom: string; seen: string[] }

/** A decoded cursor with its high-water mark and the start of the next request, both in UTC. */
export interface DecodedTicketEventCursor { position: TicketEventCursor; since: GoTime; start: GoTime }

/**
 * Decode token as Go's json.Unmarshal fills the cursor struct, then parse its times: keys match
 * case-insensitively, later keys win, unknown keys are ignored, and null leaves the zero value.
 */
export function decodeTicketEventCursor(token: string): DecodedTicketEventCursor {
  const position: TicketEventCursor = { since: '', scanFrom: '', seen: [] };
  try {
    const document = parseGoJSON(token);
    if (document.kind !== 'null') {
      if (document.kind !== 'object') throw new TicketEventCursorInvalidError();
      for (const { field, value } of goFieldMembers(document.members, ['since', 'scanFrom', 'seen'])) {
        if (value.kind === 'null') {
          if (field === 'seen') position.seen = [];
          continue;
        }
        if (field === 'seen') {
          if (value.kind !== 'array') throw new TicketEventCursorInvalidError();
          position.seen = value.elements.map(element => (element.kind === 'null' ? '' : decodeGoString(element)));
        } else if (field === 'since') position.since = decodeGoString(value);
        else position.scanFrom = decodeGoString(value);
      }
    }
    const since = parseGoTime(position.since);
    let start = since.add(-TicketEventOverlapNanoseconds);
    if (position.scanFrom) {
      const scanFrom = parseGoTime(position.scanFrom);
      if (scanFrom.isAfter(start)) start = scanFrom;
    }
    return { position, since: since.inUTC(), start: start.inUTC() };
  } catch {
    // A JSON, type, or time error is Go's errTicketEventCursorInvalid.
    throw new TicketEventCursorInvalidError();
  }
}

/** Encode position as Go's json.Marshal of the cursor struct: field order, omitempty, HTML-safe strings. */
export function encodeTicketEventCursor(position: TicketEventCursor): string {
  const members = [`"since":${encodeGoJSONValue(position.since)}`];
  if (position.scanFrom) members.push(`"scanFrom":${encodeGoJSONValue(position.scanFrom)}`);
  if (position.seen.length) members.push(`"seen":[${position.seen.map(key => encodeGoJSONValue(key)).join(',')}]`);
  return `{${members.join(',')}}`;
}

/** Name one event version as "<event id>@<created_datetime Unix microseconds>". */
export function seenEventKey(eventId: number, createdAt: GoTime): string { return `${eventId}@${createdAt.unixMicroseconds}`; }

/** Keep distinct keys recorded at or after windowStart: the newest MaximumSeenEventKeys, oldest first. */
export function pruneSeenEventKeys(keys: readonly string[], windowStart: GoTime): string[] {
  const windowStartMicroseconds = windowStart.unixMicroseconds;
  const kept: { key: string; createdAt: bigint }[] = [];
  const isKept = new Set<string>();
  for (const key of keys) {
    const separator = key.indexOf('@');
    if (separator < 0 || isKept.has(key)) continue;
    const createdText = key.slice(separator + 1);
    if (!int64Pattern.test(createdText)) continue;
    const createdAt = BigInt(createdText);
    if (createdAt < -(1n << 63n) || createdAt >= 1n << 63n || createdAt < windowStartMicroseconds) continue;
    isKept.add(key);
    kept.push({ key, createdAt });
  }
  // Array.prototype.sort is stable, as Go's sort.SliceStable.
  kept.sort((left, right) => (left.createdAt < right.createdAt ? -1 : left.createdAt > right.createdAt ? 1 : 0));
  return kept.slice(Math.max(kept.length - MaximumSeenEventKeys, 0)).map(entry => entry.key);
}
