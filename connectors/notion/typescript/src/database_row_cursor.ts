// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// The databaseRowChanged cursor document, decoded and encoded byte for byte as the Go connector does.

import { createHash } from 'node:crypto';
import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import { GoTime, parseGoTime } from './go_time.js';

export const databaseRowCursorVersion = 1;
/** Two minutes in nanoseconds. */
export const databaseRowLookbackNanoseconds = 120_000_000_000n;
/** At most this many row versions are remembered inside the lookback. */
export const maximumSeenDatabaseRows = 2000;
// Keeps the cursor under the kit's 64 KiB token limit with one millisecond timestamp per row.
const maximumSeenRowBytes = 48 << 10;
const int64Minimum = -(1n << 63n);
const int64Maximum = (1n << 63n) - 1n;
/** Go's zero time.Time, which a JSON null leaves in place. */
export const goZeroTime = parseGoTime('0001-01-01T00:00:00Z');

/** The token is not a cursor Go's json.Unmarshal would accept. */
export class DatabaseRowCursorDecodeError extends Error {
  constructor(message: string) { super(message); this.name = 'DatabaseRowCursorDecodeError'; }
}

/** The rows delivered or baselined at one last edited time, as short row-ID digests. */
export interface SeenRowGroup { lastEditedTime: GoTime; rowKeys: string[] | null }

/** The committed position: newest last edited time, recent row versions, and Notion's start cursor. */
export interface DatabaseRowCursor {
  version: number;
  dataSourceId: string;
  highWater: GoTime;
  queryFrom: GoTime | null;
  startCursor: string;
  seen: SeenRowGroup[];
}

/** A JSON number with the source text Go's decoder would see. */
class SourceNumber { constructor(readonly source: string) {} }

type Reviver = (this: unknown, key: string, value: unknown, context?: { source?: string }) => unknown;

/**
 * Decode token as Go's json.Unmarshal into the cursor struct: keys match case-insensitively, unknown keys are
 * ignored, and null leaves the zero value. Throws DatabaseRowCursorDecodeError where Go reports an error.
 */
export function decodeDatabaseRowCursor(token: string): DatabaseRowCursor {
  let document: unknown;
  const keepNumberSource: Reviver = (_key, value, context) => typeof value === 'number' ? new SourceNumber(context?.source ?? String(value)) : value;
  try { document = (JSON.parse as (text: string, reviver: Reviver) => unknown)(token, keepNumberSource); }
  catch { throw new DatabaseRowCursorDecodeError('cursor is not JSON'); }
  const cursor: DatabaseRowCursor = { version: 0, dataSourceId: '', highWater: goZeroTime, queryFrom: null, startCursor: '', seen: [] };
  if (document === null) return cursor;
  if (!isPlainObject(document)) throw new DatabaseRowCursorDecodeError('cursor is not an object');
  for (const [name, value] of foldMembers(document, ['v', 'dataSourceId', 'highWater', 'queryFrom', 'startCursor', 'seen'])) {
    if (value === null) {
      if (name === 'queryFrom') cursor.queryFrom = null;
      else if (name === 'seen') cursor.seen = [];
      continue;
    }
    switch (name) {
      case 'v': cursor.version = decodeInt64(value); break;
      case 'dataSourceId': cursor.dataSourceId = decodeString(value); break;
      case 'highWater': cursor.highWater = decodeTime(value); break;
      case 'queryFrom': cursor.queryFrom = decodeTime(value); break;
      case 'startCursor': cursor.startCursor = decodeString(value); break;
      default: cursor.seen = decodeSeen(value);
    }
  }
  return cursor;
}

/** Encode cursor as Go's json.Marshal of the cursor struct, in field order with omitempty fields. */
export function encodeDatabaseRowCursor(cursor: DatabaseRowCursor): string {
  let encoded = `{"v":${cursor.version},"dataSourceId":${encodeGoJSONValue(cursor.dataSourceId)},"highWater":"${cursor.highWater.formatRFC3339Nano()}"`;
  if (cursor.queryFrom) encoded += `,"queryFrom":"${cursor.queryFrom.formatRFC3339Nano()}"`;
  if (cursor.startCursor) encoded += `,"startCursor":${encodeGoJSONValue(cursor.startCursor)}`;
  if (cursor.seen.length) {
    const groups = cursor.seen.map(group => `{"at":"${group.lastEditedTime.formatRFC3339Nano()}","rows":${group.rowKeys === null ? 'null' : `[${group.rowKeys.map(key => encodeGoJSONValue(key)).join(',')}]`}}`);
    encoded += `,"seen":[${groups.join(',')}]`;
  }
  return `${encoded}}`;
}

/** Row-ID digests indexed by last edited time in Unix nanoseconds. */
export class SeenRows {
  private readonly keysByInstant = new Map<bigint, Set<string>>();

  /** Index the committed groups. */
  constructor(groups: readonly SeenRowGroup[]) {
    for (const group of groups) for (const key of group.rowKeys ?? []) this.addKey(group.lastEditedTime, key);
  }

  /** Report whether this row version was already delivered or baselined. */
  contains(lastEditedTime: GoTime, rowId: string): boolean {
    return this.keysByInstant.get(lastEditedTime.unixNanoseconds)?.has(seenRowKey(rowId)) ?? false;
  }

  /** Remember one row version. */
  add(lastEditedTime: GoTime, rowId: string): void { this.addKey(lastEditedTime, seenRowKey(rowId)); }

  /** Keep the newest row versions edited inside the lookback, bounded in count and encoded bytes. */
  groupsWithin(highWater: GoTime): SeenRowGroup[] {
    const lowerBound = highWater.unixNanoseconds - databaseRowLookbackNanoseconds;
    const instants = [...this.keysByInstant.keys()].filter(instant => instant >= lowerBound).sort((left, right) => left < right ? 1 : left > right ? -1 : 0);
    const groups: SeenRowGroup[] = [];
    let remaining = maximumSeenDatabaseRows; let remainingBytes = maximumSeenRowBytes;
    const keyBytes = '"",'.length + seenRowKey('').length;
    for (const instant of instants) {
      const lastEditedTime = new GoTime(instant);
      const groupBytes = '{"at":"","rows":[]},'.length + lastEditedTime.formatRFC3339Nano().length;
      const fitting = Math.min(remaining, Math.trunc((remainingBytes - groupBytes) / keyBytes));
      if (fitting <= 0) break;
      const keys = [...this.keysByInstant.get(instant)!].sort(compareUTF8).slice(0, fitting);
      remaining -= keys.length; remainingBytes -= groupBytes + keys.length * keyBytes;
      groups.push({ lastEditedTime, rowKeys: keys });
    }
    return groups.reverse();
  }

  private addKey(lastEditedTime: GoTime, key: string): void {
    const keys = this.keysByInstant.get(lastEditedTime.unixNanoseconds) ?? new Set<string>();
    keys.add(key);
    this.keysByInstant.set(lastEditedTime.unixNanoseconds, keys);
  }
}

/** The 96-bit row-ID digest: unique enough within one data source's lookback, and short. */
export function seenRowKey(rowId: string): string {
  return createHash('sha256').update(rowId).digest().subarray(0, 12).toString('base64url');
}

function compareUTF8(left: string, right: string): number { return Buffer.compare(Buffer.from(left), Buffer.from(right)); }

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) && !(value instanceof SourceNumber);
}

// Go prefers an exact field name and otherwise matches case-insensitively.
function foldMembers(document: Record<string, unknown>, names: readonly string[]): [string, unknown][] {
  const members: [string, unknown][] = [];
  for (const [key, value] of Object.entries(document)) {
    const name = names.includes(key) ? key : names.find(candidate => candidate.toLowerCase() === key.toLowerCase());
    if (name !== undefined) members.push([name, value]);
  }
  return members;
}

function decodeInt64(value: unknown): number {
  if (!(value instanceof SourceNumber) || !/^-?(0|[1-9][0-9]*)$/.test(value.source)) throw new DatabaseRowCursorDecodeError('cursor version is not an integer');
  const parsed = BigInt(value.source);
  if (parsed < int64Minimum || parsed > int64Maximum) throw new DatabaseRowCursorDecodeError('cursor version is out of range');
  // Only version 1 is ever re-encoded, so any other version collapses to an invalid marker.
  return parsed === BigInt(databaseRowCursorVersion) ? databaseRowCursorVersion : -1;
}

function decodeString(value: unknown): string {
  if (typeof value !== 'string') throw new DatabaseRowCursorDecodeError('cursor string field has another type');
  return value;
}

function decodeTime(value: unknown): GoTime {
  try { return parseGoTime(decodeString(value)); }
  catch { throw new DatabaseRowCursorDecodeError('cursor time is not RFC 3339'); }
}

function decodeSeen(value: unknown): SeenRowGroup[] {
  if (!Array.isArray(value)) throw new DatabaseRowCursorDecodeError('cursor seen is not an array');
  return value.map((item: unknown): SeenRowGroup => {
    const group: SeenRowGroup = { lastEditedTime: goZeroTime, rowKeys: null };
    if (item === null) return group;
    if (!isPlainObject(item)) throw new DatabaseRowCursorDecodeError('cursor seen group is not an object');
    for (const [name, member] of foldMembers(item, ['at', 'rows'])) {
      if (member === null) { if (name === 'rows') group.rowKeys = null; continue; }
      if (name === 'at') { group.lastEditedTime = decodeTime(member); continue; }
      if (!Array.isArray(member)) throw new DatabaseRowCursorDecodeError('cursor seen rows is not an array');
      group.rowKeys = member.map((key: unknown) => key === null ? '' : decodeString(key));
    }
    return group;
  });
}
