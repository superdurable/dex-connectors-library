// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { TriggerEvent } from '@superdurable/connectors-sdk';
import type { KeyedItem } from '@superdurable/connectors-sdk/pollingtrigger';
import { z } from 'zod';
import { ItemSchema, type Item, type ItemColumnValue } from './generated/models.js';

/**
 * An item the Trigger has not delivered before whose creation time falls after the previous poll's position,
 * minus the lookback window. An item created and then edited between two polls is still created.
 */
export const ItemChangeCreated = 'created';
/** Any other change monday.com reports by moving the item's last-updated time, such as a column value, name, or group change. */
export const ItemChangeUpdated = 'updated';
/** The derived kind of one itemChanged delivery. */
export const ItemChangeKindSchema = z.enum([ItemChangeCreated, ItemChangeUpdated]);
/** created or updated. */
export type ItemChangeKind = z.infer<typeof ItemChangeKindSchema>;

const numericIDPattern = /^[1-9][0-9]{0,19}$/;
const columnIDPattern = /^[A-Za-z0-9_-]{1,100}$/;
const columnTypePattern = /^[a-z][a-z0-9_]{0,63}$/;

/**
 * The itemChanged binding configuration that the boardIdInput unit saves in Dex Web. Changing the board of a
 * binding that already polled makes its next poll record a new baseline for the new board.
 */
export const ItemChangedTriggerConfigurationSchema = z.strictObject({
  /** The numeric ID of the watched board, the digits after /boards/ in the board's address, such as 1234567890. Required. */
  boardId: z.string({ error: 'boardId must be a numeric monday.com ID such as 1234567890' }),
}).superRefine((configuration, context) => {
  if (!numericIDPattern.test(configuration.boardId.trim())) context.addIssue({ code: 'custom', message: 'boardId must be a numeric monday.com ID such as 1234567890' });
  else if (configuration.boardId !== configuration.boardId.trim()) context.addIssue({ code: 'custom', message: 'boardId must not have surrounding whitespace' });
});
/** The itemChanged binding configuration. */
export type ItemChangedTriggerConfiguration = z.infer<typeof ItemChangedTriggerConfigurationSchema>;

/**
 * One active item created or changed on the watched board, as monday.com returned it in the poll's items page
 * with every column value; field names match the Go connector, so Go and TypeScript replicas share a binding's inbox.
 */
export const ItemChangedEventSchema = z.object({
  /** The watched board. */ boardId: z.string(),
  /** The derived change kind. */ changeKind: ItemChangeKindSchema,
  /** The item with its group, timestamps, and every column value. */ item: ItemSchema,
});
/** One active item created or changed on the watched board. */
export type ItemChangedEvent = z.infer<typeof ItemChangedEventSchema>;

/** Bounds Text of one column value, as Go's MaxColumnTextBytes. */
export const MaxColumnTextBytes = 4096;
/** Bounds the raw JSON kept for one column value, as Go's MaxColumnValueBytes. */
export const MaxColumnValueBytes = 16384;
const nanosecondsPerSecond = 1_000_000_000n;
/** Go's zero time.Time, 0001-01-01T00:00:00Z, in nanoseconds since the Unix epoch. */
export const GoZeroTimeNanoseconds = -62_135_596_800n * nanosecondsPerSecond;
// unicode.IsSpace, which Go's bytes.TrimSpace uses.
const goSpace = '\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000';
const fraction = '(?:[.,]([0-9]+))?';
// The four layouts of Go's mondayTimeLayouts, in order; a zone abbreviation counts as UTC.
const providerTimeLayouts = [
  new RegExp(`^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})${fraction}(Z|([+-])([0-9]{2}):([0-9]{2}))$`),
  new RegExp(`^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})${fraction}(Z|([+-])([0-9]{2})([0-9]{2}))$`),
  new RegExp(`^([0-9]{4})-([0-9]{2})-([0-9]{2}) ([0-9]{2}):([0-9]{2}):([0-9]{2})${fraction} ([A-Z]{3}|[A-Z]{3}T|[A-Z]{4}T|WITA|ChST|MeST)$`),
  new RegExp(`^([0-9]{4})-([0-9]{2})-([0-9]{2}) ([0-9]{2}):([0-9]{2}):([0-9]{2})${fraction}$`),
];
const strictRFC3339 = /^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:\.([0-9]+))?(Z|([+-])([0-9]{2}):([0-9]{2}))$/;

/** Replace lone surrogates with U+FFFD, as Go's decoder does for invalid escapes. */
export function wellFormed(text: string): string {
  return text.replace(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g, '\ufffd');
}

function escapeHTML(serialized: string): string {
  return serialized.replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
}

/** Encode a string as Go's encoding/json does, escaping HTML characters and U+2028 and U+2029. */
export function goJSONString(value: string): string {
  return escapeHTML(JSON.stringify(wellFormed(value)));
}

/** Trim leading and trailing Unicode white space as Go's bytes.TrimSpace does. */
export function goTrimSpace(value: string): string {
  let start = 0, end = value.length;
  while (start < end && goSpace.includes(value[start]!)) start++;
  while (end > start && goSpace.includes(value[end - 1]!)) end--;
  return value.slice(start, end);
}

/** Compact valid JSON text and escape HTML characters, as Go marshals a json.RawMessage. */
export function goCompactJSON(raw: string): string {
  let compacted = '', isInString = false, isEscaped = false;
  for (const character of raw) {
    if (isInString) {
      if (isEscaped) isEscaped = false;
      else if (character === '\\') isEscaped = true;
      else if (character === '"') isInString = false;
    } else if (character === '"') isInString = true;
    else if (' \t\n\r'.includes(character)) continue;
    compacted += character;
  }
  return escapeHTML(compacted);
}

function instantNanoseconds(match: RegExpExecArray, offsetSeconds: number): bigint | undefined {
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number) as [number, number, number, number, number, number];
  if (hour > 23 || minute > 59 || second > 59 || month < 1 || month > 12 || day < 1) return undefined;
  const isLeap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  if (day > [31, isLeap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1]!) return undefined;
  const instant = new Date(0);
  instant.setUTCFullYear(year, month - 1, day);
  instant.setUTCHours(hour, minute, second, 0);
  const seconds = BigInt(instant.getTime() / 1000 - offsetSeconds);
  return seconds * nanosecondsPerSecond + BigInt((match[7] ?? '').slice(0, 9).padEnd(9, '0'));
}

/** Return nanoseconds since the Unix epoch of a monday.com timestamp, or undefined as Go's zero time. */
export function parseProviderTime(value: unknown): bigint | undefined {
  if (typeof value !== 'string' || !value) return undefined;
  for (const [index, layout] of providerTimeLayouts.entries()) {
    const match = layout.exec(value);
    if (!match) continue;
    let offsetSeconds = 0;
    if (index < 2 && match[8] !== 'Z') {
      const zoneHour = Number(match[10]), zoneMinute = Number(match[11]);
      if (zoneHour > 23 || zoneMinute > 59) continue;
      offsetSeconds = (zoneHour * 3600 + zoneMinute * 60) * (match[9] === '+' ? 1 : -1);
    }
    const instant = instantNanoseconds(match, offsetSeconds);
    if (instant !== undefined) return instant;
  }
  return undefined;
}

/** Parse a cursor watermark as Go's time.Time UnmarshalJSON does; undefined when Go would refuse it. */
export function parseCursorTime(value: string): bigint | undefined {
  const match = strictRFC3339.exec(value);
  if (!match) return undefined;
  let offsetSeconds = 0;
  if (match[8] !== 'Z') {
    const zoneHour = Number(match[10]), zoneMinute = Number(match[11]);
    if (zoneHour > 23 || zoneMinute > 59) return undefined;
    offsetSeconds = (zoneHour * 3600 + zoneMinute * 60) * (match[9] === '+' ? 1 : -1);
  }
  return instantNanoseconds(match, offsetSeconds);
}

/** Format a UTC instant as Go's time.Time MarshalJSON does; undefined is Go's zero time. */
export function formatGoTime(nanoseconds: bigint | undefined): string {
  const instant = nanoseconds ?? GoZeroTimeNanoseconds;
  let seconds = instant / nanosecondsPerSecond, remainder = instant % nanosecondsPerSecond;
  if (remainder < 0n) { seconds -= 1n; remainder += nanosecondsPerSecond; }
  const text = new Date(Number(seconds) * 1000).toISOString().slice(0, 19);
  return text + (remainder ? `.${remainder.toString().padStart(9, '0').replace(/0+$/, '')}` : '') + 'Z';
}

function truncateUTF8(value: string, maximumBytes: number): [string, boolean] {
  const encoded = Buffer.from(wellFormed(value), 'utf8');
  if (encoded.length <= maximumBytes) return [encoded.toString('utf8'), false];
  let cut = maximumBytes;
  while (cut > 0 && (encoded[cut]! & 0xc0) === 0x80) cut--;
  return [encoded.subarray(0, cut).toString('utf8'), true];
}

function stringField(resource: Record<string, unknown>, key: string): string {
  const value = resource[key];
  if (value === null || value === undefined) return '';
  if (typeof value !== 'string') throw new TypeError(`item ${key} is not a string`);
  return value;
}

function objectValue(value: unknown, message: string): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError(message);
  return value as Record<string, unknown>;
}

/** One item a poll read, with the Go JSON its content hash, snapshot entry, and event ID use. */
export class PolledItem implements KeyedItem {
  /** Keep the decoded item, its instants, and its Go JSON. */
  constructor(readonly item: Item, readonly createdAt: bigint | undefined, readonly updatedAt: bigint | undefined, readonly encoded: Uint8Array) {}

  /** The decimal item ID. */ snapshotKey(): string { return this.item.id; }
  /** The first 16 bytes of SHA-256 over the item's Go JSON. */
  snapshotHash(): string { return createHash('sha256').update(this.encoded).digest().subarray(0, 16).toString('hex'); }
  /** The item's last-updated time, so entries leave the snapshot behind the lookback window. */
  snapshotValue(): Uint8Array { return Buffer.from(`{"updatedAt":"${formatGoTime(this.updatedAt)}"}`, 'utf8'); }
}

/** Decode one items_page item as Go's decodeItemResource does; throws when Go would refuse it. */
export function decodePolledItem(value: unknown): PolledItem {
  const resource = objectValue(value, 'item is not an object');
  const id = stringField(resource, 'id'), name = wellFormed(stringField(resource, 'name'));
  if (!numericIDPattern.test(id)) throw new TypeError('item ID is not a monday.com numeric ID');
  const state = stringField(resource, 'state');
  if (!['', 'active', 'archived', 'deleted'].includes(state)) throw new TypeError('item state is not a monday.com state');
  let url = stringField(resource, 'url');
  try { const parsed = new URL(url); if (parsed.protocol !== 'https:' || !parsed.hostname || parsed.username || parsed.password) url = ''; } catch { url = ''; }
  let boardId = '';
  if (resource.board !== null && resource.board !== undefined) {
    boardId = stringField(objectValue(resource.board, 'item board ID is not a monday.com numeric ID'), 'id');
    if (!numericIDPattern.test(boardId)) throw new TypeError('item board ID is not a monday.com numeric ID');
  }
  let group: Item['group'];
  if (resource.group !== null && resource.group !== undefined) {
    const groupResource = objectValue(resource.group, 'item group ID is not a monday.com group ID');
    const groupId = stringField(groupResource, 'id'), title = wellFormed(stringField(groupResource, 'title'));
    if (!columnIDPattern.test(groupId)) throw new TypeError('item group ID is not a monday.com group ID');
    group = { id: groupId, ...(title ? { title } : {}) };
  }
  const creatorId = stringField(resource, 'creator_id');
  if (creatorId && !numericIDPattern.test(creatorId)) throw new TypeError('item creator ID is not a monday.com numeric ID');
  const createdAt = parseProviderTime(resource.created_at), updatedAt = parseProviderTime(resource.updated_at);
  const columnResources = resource.column_values ?? [];
  if (!Array.isArray(columnResources)) throw new TypeError('item column values are not a list');
  const columns = columnResources.map(decodeColumnValue);
  const fields = [`"id":${goJSONString(id)}`, `"name":${goJSONString(name)}`];
  if (state) fields.push(`"state":${goJSONString(state)}`);
  if (boardId) fields.push(`"boardId":${goJSONString(boardId)}`);
  if (group) fields.push(`"group":{"id":${goJSONString(group.id)}${group.title ? `,"title":${goJSONString(group.title)}` : ''}}`);
  if (creatorId) fields.push(`"creatorId":${goJSONString(creatorId)}`);
  if (url) fields.push(`"url":${goJSONString(url)}`);
  if (createdAt !== undefined) fields.push(`"createdAt":"${formatGoTime(createdAt)}"`);
  if (updatedAt !== undefined) fields.push(`"updatedAt":"${formatGoTime(updatedAt)}"`);
  if (columns.length) fields.push(`"columnValues":[${columns.map(([, encoded]) => encoded).join(',')}]`);
  const item: Item = {
    id, name, ...(state ? { state } : {}), ...(boardId ? { boardId } : {}), ...(group ? { group } : {}), ...(creatorId ? { creatorId } : {}), ...(url ? { url } : {}),
    ...(createdAt !== undefined ? { createdAt: formatGoTime(createdAt) } : {}), ...(updatedAt !== undefined ? { updatedAt: formatGoTime(updatedAt) } : {}),
    ...(columns.length ? { columnValues: columns.map(([column]) => column) } : {}),
  };
  return new PolledItem(item, createdAt, updatedAt, Buffer.from(`{${fields.join(',')}}`, 'utf8'));
}

/** Bound one column value as Go's decodeColumnValueResource does and return it with its Go JSON. */
function decodeColumnValue(value: unknown): [ItemColumnValue, string] {
  const resource = objectValue(value, 'column is not an object');
  const id = stringField(resource, 'id'), type = stringField(resource, 'type');
  if (!columnIDPattern.test(id)) throw new TypeError('column ID is not a monday.com column ID');
  if (!columnTypePattern.test(type)) throw new TypeError(`column ${id} type is not a monday.com column type`);
  const [text, isTextTruncated] = truncateUTF8(stringField(resource, 'text'), MaxColumnTextBytes);
  let rawText: string | undefined;
  if (typeof resource.value === 'string') {
    rawText = goTrimSpace(wellFormed(resource.value));
    if (rawText === '' || rawText === 'null') rawText = undefined;
  } else if (resource.value !== null && resource.value !== undefined) {
    // A non-string value arrives already parsed; its re-encoding keeps key order.
    rawText = JSON.stringify(resource.value);
  }
  let parsed: unknown, isValueOmitted = false, encodedValue = '';
  if (rawText !== undefined) {
    try { parsed = JSON.parse(rawText); } catch { throw new TypeError(`column ${id} value is not JSON`); }
    if (Buffer.byteLength(rawText, 'utf8') > MaxColumnValueBytes) { parsed = undefined; isValueOmitted = true; }
    else encodedValue = goCompactJSON(rawText);
  }
  const fields = [`"id":${goJSONString(id)}`, `"type":${goJSONString(type)}`];
  if (text) fields.push(`"text":${goJSONString(text)}`);
  if (encodedValue) fields.push(`"value":${encodedValue}`);
  if (isTextTruncated) fields.push('"textTruncated":true');
  if (isValueOmitted) fields.push('"valueOmitted":true');
  const column: ItemColumnValue = {
    id, type, ...(text ? { text } : {}), ...(encodedValue ? { value: parsed } : {}),
    ...(isTextTruncated ? { textTruncated: true } : {}), ...(isValueOmitted ? { valueOmitted: true } : {}),
  };
  return [column, `{${fields.join(',')}}`];
}

/** Convert one changed item; its ID is a pure function of the board, item ID, and item content. */
export function newItemChangedTriggerEvent(boardId: string, polled: PolledItem, kind: ItemChangeKind): TriggerEvent<ItemChangedEvent> {
  const digest = createHash('sha256').update(`${boardId}\u0000${polled.item.id}\u0000${polled.snapshotHash()}`, 'utf8').digest();
  return {
    id: `monday-item-${digest.subarray(0, 16).toString('hex')}`,
    occurredAt: new Date(Number((polled.updatedAt ?? 0n) / 1_000_000n)),
    payload: { boardId, changeKind: kind, item: polled.item },
  };
}
