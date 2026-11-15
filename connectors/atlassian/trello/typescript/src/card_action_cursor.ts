// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Cursor } from '@superdurable/connectors-sdk/pollingtrigger';

/** Re-read window, in nanoseconds, because actions recorded in the same moment can surface late. */
export const actionLookbackNanoseconds = 60_000_000_000n;
/** Keeps the cursor token far below the poll kit's 64 KiB bound. */
export const maximumSeenActionIds = 2000;

const nanosecondsPerSecond = 1_000_000_000n;
const trelloIdPattern = /^[0-9a-fA-F]{24}$/;
// Go's time.RFC3339Nano parse: a dot or comma fraction with at least one digit, and Z or an offset.
const rfc3339Pattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(?:(Z)|([+-])(\d{2}):(\d{2}))$/;
const cursorFields = { boardid: 'boardId', watermark: 'watermark', seenactionids: 'seenActionIds', flooractionid: 'floorActionId' } as const;

/** An instant in Unix nanoseconds and the UTC offset, in seconds, it was written with. */
export interface GoTime { unixNanoseconds: bigint; offsetSeconds: number }

/**
 * The committed position, field for field as the Go connector's cardActionCursor. watermark is the newest
 * delivered action time, blank when the board had no card action at the baseline; seenActionIds are the
 * sorted lowercase IDs delivered within the lookback of watermark; every action ID at or below floorActionId
 * counts as delivered.
 */
export interface CardActionCursor { boardId: string; watermark: string; seenActionIds: string[]; floorActionId: string }

/** Parse an RFC 3339 time as Go's time.Parse(time.RFC3339Nano) does; undefined when Go rejects it. */
export function parseGoTime(value: string): GoTime | undefined {
  const parts = rfc3339Pattern.exec(value);
  if (!parts) return undefined;
  const [year, month, day, hour, minute, second] = parts.slice(1, 7).map(Number) as [number, number, number, number, number, number];
  if (year < 1 || month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59) return undefined;
  const daysInMonth = new Date(Date.UTC(2000, month, 0)).getUTCDate() - (month === 2 && !isLeapYear(year) ? 1 : 0);
  if (day < 1 || day > daysInMonth) return undefined;
  const fraction = parts[7] ?? '';
  let offsetSeconds = 0;
  if (parts[9]) {
    const offsetHours = Number(parts[10]), offsetMinutes = Number(parts[11]);
    if (offsetHours > 24 || offsetMinutes > 59) return undefined;
    offsetSeconds = (offsetHours * 3600 + offsetMinutes * 60) * (parts[9] === '-' ? -1 : 1);
  }
  const utc = new Date(0);
  utc.setUTCFullYear(year, month - 1, day);
  utc.setUTCHours(hour, minute, second, 0);
  const seconds = BigInt(utc.getTime() / 1000 - offsetSeconds);
  return { unixNanoseconds: seconds * nanosecondsPerSecond + BigInt((fraction + '000000000').slice(0, 9)), offsetSeconds };
}

/** Format as Go's 2006-01-02T15:04:05.000Z07:00 layout, keeping the moment's offset. */
export function formatTrelloTime(moment: GoTime): string {
  const local = moment.unixNanoseconds + BigInt(moment.offsetSeconds) * nanosecondsPerSecond;
  const text = new Date(Number(floorDivide(local, 1_000_000n))).toISOString().slice(0, 23);
  if (moment.offsetSeconds === 0) return `${text}Z`;
  const minutes = Math.abs(moment.offsetSeconds) / 60;
  return `${text}${moment.offsetSeconds < 0 ? '-' : '+'}${String(Math.floor(minutes / 60)).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}`;
}

/** Format as Go's time.Time JSON in UTC: RFC 3339 with trailing fraction zeros trimmed. */
export function formatGoJSONTime(unixNanoseconds: bigint): string {
  const seconds = floorDivide(unixNanoseconds, nanosecondsPerSecond);
  const fraction = String(unixNanoseconds - seconds * nanosecondsPerSecond).padStart(9, '0').replace(/0+$/, '');
  return new Date(Number(seconds) * 1000).toISOString().slice(0, 19) + (fraction ? `.${fraction}` : '') + 'Z';
}

/** Encode position as Go's json.Marshal does, with the same field order, omitted empty fields, and HTML escapes. */
export function encodeCardActionCursor(position: CardActionCursor): Cursor {
  const document: Record<string, unknown> = { boardId: position.boardId };
  if (position.watermark) document.watermark = position.watermark;
  if (position.seenActionIds.length) document.seenActionIds = position.seenActionIds;
  if (position.floorActionId) document.floorActionId = position.floorActionId;
  const token = JSON.stringify(document).replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
  return { token };
}

/**
 * Decode a stored token as Go's strict json.Decoder does and check it belongs to boardId: keys match
 * case-insensitively, an unknown key fails, null leaves a field empty, and text after the first JSON value is
 * ignored. Throws TypeError for an undecodable token, another board, an invalid watermark, or an action ID that
 * is not a lowercase Trello ID.
 */
export function decodeCardActionCursor(token: string, boardId: string): CardActionCursor {
  const undecodable = () => new TypeError('Trello card action cursor is not decodable');
  const position: CardActionCursor = { boardId: '', watermark: '', seenActionIds: [], floorActionId: '' };
  let document: unknown;
  try { document = JSON.parse(firstJSONValue(token)); } catch { throw undecodable(); }
  if (document !== null) {
    if (typeof document !== 'object' || Array.isArray(document)) throw undecodable();
    for (const [key, value] of Object.entries(document as Record<string, unknown>)) {
      const field = cursorFields[(/^[\x20-\x7e]*$/.test(key) ? key.toLowerCase() : key) as keyof typeof cursorFields];
      if (!field) throw undecodable();
      if (field === 'seenActionIds') {
        if (value !== null && (!Array.isArray(value) || value.some(item => item !== null && typeof item !== 'string'))) throw undecodable();
        position.seenActionIds = ((value ?? []) as (string | null)[]).map(item => item ?? '');
      } else {
        if (value !== null && typeof value !== 'string') throw undecodable();
        position[field] = (value ?? '') as string;
      }
    }
  }
  if (position.boardId !== boardId) throw new TypeError('Trello card action cursor belongs to another board');
  if (position.watermark && !parseGoTime(position.watermark)) throw new TypeError('Trello card action cursor has an invalid watermark');
  for (const actionId of [...position.seenActionIds, position.floorActionId]) {
    if (actionId && (!trelloIdPattern.test(actionId) || actionId !== actionId.toLowerCase())) throw new TypeError('Trello card action cursor has an invalid action ID');
  }
  return position;
}

/**
 * Merge previous and added IDs and keep those created inside the lookback before watermark, the newest
 * maximumSeenActionIds, sorted. Returns the floor raised to the largest ID dropped over that bound, or blank
 * once the floor's time is outside the window. An undefined watermark is Go's zero time, before every ID.
 */
export function seenActionIdsWithin(previous: readonly string[], floor: string, added: readonly string[], watermark: GoTime | undefined): [string[], string] {
  const horizon = watermark ? watermark.unixNanoseconds - actionLookbackNanoseconds - nanosecondsPerSecond : undefined;
  const merged = new Set([...previous, ...added.map(actionId => actionId.toLowerCase())]);
  let kept = [...merged].filter(actionId => isInside(actionId, horizon)).sort(compareASCII);
  if (kept.length > maximumSeenActionIds) {
    const dropped = kept[kept.length - maximumSeenActionIds - 1]!;
    if (compareASCII(dropped, floor) > 0) floor = dropped;
    kept = kept.slice(kept.length - maximumSeenActionIds);
  }
  if (!isInside(floor, horizon)) floor = '';
  return [kept, floor];
}

/** Return the creation second Trello's Mongo-style IDs carry in their first four bytes. */
export function objectIdSeconds(actionId: string): number | undefined {
  return trelloIdPattern.test(actionId) ? Number.parseInt(actionId.slice(0, 8), 16) : undefined;
}

/** Compare as Go compares strings, byte by byte; IDs and times here are ASCII. */
export function compareASCII(left: string, right: string): number { return left < right ? -1 : left > right ? 1 : 0; }

function isInside(actionId: string, horizon: bigint | undefined): boolean {
  const created = objectIdSeconds(actionId);
  return created !== undefined && (horizon === undefined || BigInt(created) * nanosecondsPerSecond >= horizon);
}

function isLeapYear(year: number): boolean { return (year % 4 === 0 && year % 100 !== 0) || year % 400 === 0; }

function floorDivide(value: bigint, divisor: bigint): bigint {
  const quotient = value / divisor;
  return value % divisor < 0n ? quotient - 1n : quotient;
}

/** Return the first JSON object of text after JSON whitespace, as Go's json.Decoder reads only one value. */
function firstJSONValue(text: string): string {
  const start = text.search(/[^ \t\r\n]/);
  if (start < 0 || text[start] !== '{') return text;
  let depth = 0, isInString = false, isEscaped = false;
  for (let index = start; index < text.length; index++) {
    const character = text[index]!;
    if (isInString) {
      if (isEscaped) isEscaped = false;
      else if (character === '\\') isEscaped = true;
      else if (character === '"') isInString = false;
    } else if (character === '"') isInString = true;
    else if (character === '{' || character === '[') depth++;
    else if (character === '}' || character === ']') {
      depth--;
      if (depth === 0) return text.slice(start, index + 1);
    }
  }
  return text;
}
