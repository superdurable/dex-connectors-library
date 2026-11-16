// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { cursor as searchCursorPattern, recordId as recordIdPattern, type ObjectType } from './operations.js';

/** Re-reads recent changes, because HubSpot indexes new and updated records after a delay. */
export const searchIndexLagWindowMs = 2 * 60 * 1000;
/** Keeps the cursor document well under the kit's 64 KiB token bound. */
export const maximumSeenRecordVersions = 1500;

/** The committed Trigger cursor, encoded as the Go connector's JSON byte for byte; all times are Unix milliseconds. */
export interface CRMObjectChangeCursor {
  objectType: ObjectType;
  /** The baseline's newest change; nothing at or before it is delivered. */ floorMs: number;
  highWaterMs: number;
  /** The GTE bound of the next search. */ anchorMs: number;
  /** HubSpot's offset, set only while over a page of records share anchorMs. */ after: string;
  /** Each record delivered inside the lag window and its delivered version. */ seen: Map<string, number>;
  /** The newest version pruned from seen for capacity, not for age; the lag-window re-read starts after it. */ droppedThroughMs: number;
}

/** Where the next caught-up search starts: inside the lag window, never in the baseline, and after any pruned version. */
export function lagWindowStart(position: CRMObjectChangeCursor): number {
  return Math.max(position.floorMs + 1, position.highWaterMs - searchIndexLagWindowMs, position.droppedThroughMs + 1);
}

/**
 * Keep the newest versions inside the lag window. When more remain than seen holds, drop the oldest and move
 * droppedThroughMs past them, so a burst is not delivered again on every poll.
 */
export function pruneSeen(position: CRMObjectChangeCursor): void {
  const windowStart = lagWindowStart(position);
  const kept = [...position.seen].filter(([, modified]) => modified >= windowStart);
  kept.sort(([leftID, left], [rightID, right]) => (left !== right ? right - left : compareDecimalIDs(leftID, rightID)));
  if (kept.length > maximumSeenRecordVersions) {
    position.droppedThroughMs = Math.max(position.droppedThroughMs, kept[maximumSeenRecordVersions]![1]);
    kept.length = maximumSeenRecordVersions;
  }
  position.seen = new Map(kept);
}

function compareDecimalIDs(left: string, right: string): number {
  if (left.length !== right.length) return left.length - right.length;
  return left < right ? -1 : left > right ? 1 : 0;
}

/** Encode the cursor as Go's json.Marshal encodes the struct: field order, omitted empty fields, sorted seen keys. */
export function encodeCRMObjectChangeCursor(position: CRMObjectChangeCursor): Cursor {
  let token = `{"objectType":${JSON.stringify(position.objectType)},"floorMs":${position.floorMs},"highWaterMs":${position.highWaterMs},"anchorMs":${position.anchorMs}`;
  if (position.after) token += `,"after":${JSON.stringify(position.after)}`;
  if (position.seen.size > 0) {
    const keys = [...position.seen.keys()].sort((left, right) => (left < right ? -1 : left > right ? 1 : 0));
    token += `,"seen":{${keys.map(key => `${JSON.stringify(key)}:${position.seen.get(key)}`).join(',')}}`;
  }
  if (position.droppedThroughMs !== 0) token += `,"droppedThroughMs":${position.droppedThroughMs}`;
  return { token: `${token}}` };
}

const fieldNames = ['objectType', 'floorMs', 'highWaterMs', 'anchorMs', 'after', 'seen', 'droppedThroughMs'] as const;

/** Decode and validate a stored cursor, which is untrusted persisted state, as the Go connector does. */
export function decodeCRMObjectChangeCursor(token: string): CRMObjectChangeCursor {
  const position: CRMObjectChangeCursor = { objectType: '' as ObjectType, floorMs: 0, highWaterMs: 0, anchorMs: 0, after: '', seen: new Map(), droppedThroughMs: 0 };
  try {
    const decoded: unknown = JSON.parse(token, markNonIntegerLiterals as never);
    if (decoded !== null) {
      if (typeof decoded !== 'object' || Array.isArray(decoded)) throw new TypeError('not an object');
      for (const [key, value] of Object.entries(decoded as Record<string, unknown>)) {
        const field = fieldNames.find(name => name === key) ?? fieldNames.find(name => name.toLowerCase() === key.toLowerCase());
        if (!field || value === null) continue;
        if (field === 'objectType' || field === 'after') position[field] = goString(value) as ObjectType;
        else if (field === 'seen') position.seen = goSeen(value);
        else position[field] = goInteger(value);
      }
    }
  } catch { throw new Error('HubSpot change cursor is not decodable'); }
  if (!['contacts', 'companies', 'deals'].includes(position.objectType)) throw new Error('HubSpot change cursor has no valid object type');
  if (position.floorMs < 0 || position.highWaterMs < position.floorMs || position.anchorMs <= position.floorMs || position.droppedThroughMs < 0 || position.droppedThroughMs > position.highWaterMs) {
    throw new Error('HubSpot change cursor times are inconsistent');
  }
  if (position.after && !searchCursorPattern.test(position.after)) throw new Error('HubSpot change cursor has an invalid paging offset');
  if (position.seen.size > maximumSeenRecordVersions) throw new Error('HubSpot change cursor holds too many record versions');
  for (const objectID of position.seen.keys()) if (!recordIdPattern.test(objectID)) throw new Error('HubSpot change cursor has an invalid record ID');
  return position;
}

/** Go rejects 1.0 and 1e3 for an int64 field; a non-integer literal becomes a marker that goInteger rejects. */
const nonIntegerLiteral = Symbol('nonIntegerLiteral');
const integerLiteral = /^-?(?:0|[1-9][0-9]*)$/;

function markNonIntegerLiterals(_key: string, value: unknown, context?: { source?: string }): unknown {
  if (typeof value === 'number' && context?.source !== undefined && !integerLiteral.test(context.source)) return nonIntegerLiteral;
  return value;
}

function goString(value: unknown): string {
  if (typeof value !== 'string') throw new TypeError('not a string');
  return value;
}

/** An int64 field; a fraction or exponent is rejected as Go rejects it, and values beyond 2^53 count as invalid. */
function goInteger(value: unknown): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('not an integer');
  return value;
}

function goSeen(value: unknown): Map<string, number> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new TypeError('not an object');
  return new Map(Object.entries(value as Record<string, unknown>).map(([objectID, modified]) => [objectID, modified === null ? 0 : goInteger(modified)]));
}
