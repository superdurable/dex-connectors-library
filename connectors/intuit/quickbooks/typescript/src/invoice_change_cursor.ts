// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Cursor } from '@superdurable/connectors-sdk/pollingtrigger';

// Times are bigint nanoseconds since the Unix epoch, so window arithmetic matches Go's time.Time exactly.
export const second = 1_000_000_000n;
/** How far before the newest delivered LastUpdatedTime each invoiceChanged poll rereads, in milliseconds. */
export const InvoiceChangeLookbackMs = 30_000;
const lookback = BigInt(InvoiceChangeLookbackMs) * 1_000_000n;
/** The most invoice versions the invoiceChanged cursor remembers inside the lookback window. */
export const MaximumSeenInvoiceVersions = 1000;
// Rereads part of the previous page, since QuickBooks leaves the order of equal timestamps unspecified.
const invoiceTiePageOverlap = 20;
// Go's zero time.Time, 0001-01-01T00:00:00Z, the "no watermark" position.
export const zeroTime = -62_135_596_800n * second;

const realmIDPattern = /^[1-9][0-9]{0,24}$/;
const goIntegerPattern = /^[+-]?[0-9]+$/;
// Go's time.Parse(time.RFC3339) falls back to its layout parser, which also takes a one-digit hour and a zone offset
// of up to 24 hours or 60 minutes.
const rfc3339Pattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/;
const int64Minimum = -(1n << 63n);
const int64Maximum = (1n << 63n) - 1n;

/** Thrown for an invoiceChanged cursor token that is not a valid Go cursor document. */
export class InvoiceChangeCursorInvalidError extends Error {
  /** Describe the rejected token without echoing it. */
  constructor(message = 'QuickBooks invoice cursor is not decodable') { super(message); this.name = 'InvoiceChangeCursorInvalidError'; }
}

/** The committed cursor fields in Go's struct order, as decoded from the token. */
export interface InvoiceChangeCursorPosition { realmId: string; environment: string; since: string; from: string; seen: string[]; offset: number }

/** A decoded cursor position; seen maps "<Id>:<SyncToken>" to its LastUpdatedTime second in nanoseconds. */
export class InvoiceWindow {
  /** Create a window; the defaults are the "no watermark" position. */
  constructor(public since = zeroTime, public from = zeroTime, public seen = new Map<string, bigint>(), public offset = 0) {}

  /**
   * Record a page of [version key, second] and return the next window. The lower bound never moves back; the page
   * offset continues only while the lower bound stays put.
   */
  advance(invoices: ReadonlyArray<readonly [string, bigint]>, isFullPage: boolean): InvoiceWindow {
    const following = new InvoiceWindow(this.since, this.from, new Map(this.seen));
    for (const [key, updatedAt] of invoices) {
      following.seen.set(key, updatedAt);
      if (updatedAt > following.since) following.since = updatedAt;
    }
    if (following.since === zeroTime) return following;
    const floor = following.since - lookback;
    if (floor > following.from) following.from = floor;
    following.pruneToBound();
    if (isFullPage && following.from === this.from) following.offset = Math.max(0, this.offset + invoices.length - invoiceTiePageOverlap);
    return following;
  }

  /**
   * Drop versions below the lower bound, then whole oldest seconds while too many remain. A single second with more
   * versions than the bound moves the window past it.
   */
  pruneToBound(): void {
    const seconds = new Map<bigint, number>();
    for (const [key, updatedAt] of this.seen) {
      if (updatedAt < this.from) { this.seen.delete(key); continue; }
      seconds.set(updatedAt, (seconds.get(updatedAt) ?? 0) + 1);
    }
    let ordered = [...seconds.keys()].sort((left, right) => (left < right ? -1 : left > right ? 1 : 0));
    let remaining = this.seen.size;
    while (remaining > MaximumSeenInvoiceVersions) {
      remaining -= seconds.get(ordered[0]!)!;
      if (ordered.length === 1) {
        this.since = ordered[0]! + second;
        this.from = this.since;
        this.seen = new Map();
        return;
      }
      this.from = ordered[1]!;
      for (const [key, updatedAt] of this.seen) if (updatedAt < this.from) this.seen.delete(key);
      ordered = ordered.slice(1);
    }
  }
}

const floorDivide = (value: bigint, divisor: bigint): bigint => (value >= 0n || value % divisor === 0n ? value / divisor : value / divisor - 1n);

/** Format a time as Go's Format(time.RFC3339) does for a UTC time: whole seconds and Z. */
export function formatRFC3339Seconds(nanoseconds: bigint): string {
  return `${new Date(Number(floorDivide(nanoseconds, second)) * 1000).toISOString().slice(0, 19)}Z`;
}

/** Parse a Go-JSON UTC timestamp, such as Invoice.updatedAt, to nanoseconds since the Unix epoch. */
export function parseRFC3339Nanoseconds(text: string): bigint {
  const parts = rfc3339Pattern.exec(text);
  if (!parts) throw new TypeError('not an RFC 3339 time');
  const [year, month, day, hour, minute, secondOfMinute] = parts.slice(1, 7).map(Number) as [number, number, number, number, number, number];
  const zone = parts[8]!;
  const offsetHours = zone === 'Z' ? 0 : Number(zone.slice(1, 3));
  const offsetMinutes = zone === 'Z' ? 0 : Number(zone.slice(4, 6));
  if (month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || secondOfMinute > 59 || offsetHours > 24 || offsetMinutes > 60) throw new TypeError('not an RFC 3339 time');
  const date = new Date(0);
  date.setUTCFullYear(year, month - 1, day);
  if (date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) throw new TypeError('not an RFC 3339 time');
  date.setUTCHours(hour, minute, secondOfMinute, 0);
  const sign = zone.startsWith('-') ? -1 : 1;
  const utcSeconds = BigInt(date.getTime() / 1000 - sign * (offsetHours * 3600 + offsetMinutes * 60));
  return utcSeconds * second + BigInt((parts[7] ?? '').slice(0, 9).padEnd(9, '0'));
}

// Replace each unpaired surrogate with U+FFFD, as Go's encoder and decoder do.
const wellFormed = (text: string): string => text.replace(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g, '\uFFFD');

// Escape as Go's json.Marshal does: HTML-safe, line and paragraph separators escaped, lone surrogates replaced.
function encodeGoJSONString(text: string): string {
  return JSON.stringify(wellFormed(text)).replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
}

// Go's sort.Strings compares UTF-8 bytes.
const compareUTF8 = (left: string, right: string): number => Buffer.compare(Buffer.from(left), Buffer.from(right));

/** Encode the cursor as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings. */
export function encodeInvoiceChangeCursor(realmId: string, environment: string, window: InvoiceWindow): Cursor {
  const members = [`"realmId":${encodeGoJSONString(realmId)}`, `"environment":${encodeGoJSONString(environment)}`];
  if (window.since !== zeroTime) {
    members.push(`"since":${encodeGoJSONString(formatRFC3339Seconds(window.since))}`, `"from":${encodeGoJSONString(formatRFC3339Seconds(window.from))}`);
  }
  const seen = [...window.seen].map(([key, updatedAt]) => `${floorDivide(updatedAt, second)}/${key}`).sort(compareUTF8);
  if (seen.length) members.push(`"seen":[${seen.map(encodeGoJSONString).join(',')}]`);
  if (window.offset) members.push(`"offset":${window.offset}`);
  return { token: `{${members.join(',')}}` };
}

const stringFields: Record<string, 'realmId' | 'environment' | 'since' | 'from'> = { realmid: 'realmId', environment: 'environment', since: 'since', from: 'from' };

// Go's encoding/json matches keys with Unicode simple folding; only the Kelvin sign and the long s fold to ASCII.
const goFieldKey = (name: string): string => name.toLowerCase().replaceAll('\u017f', 's');

// Skip one JSON value of a document JSON.parse already accepted, returning the index after it.
function skipJSONValue(text: string, index: number): number {
  let depth = 0;
  do {
    const character = text[index]!;
    if (character === '"') {
      for (index++; text[index] !== '"'; index++) if (text[index] === '\\') index++;
      index++;
    } else if (character === '{' || character === '[') { depth++; index++; }
    else if (character === '}' || character === ']') { depth--; index++; }
    else if (depth > 0) index++;
    else { while (index < text.length && !/[\s,}\]]/.test(text[index]!)) index++; }
  } while (depth > 0);
  return index;
}

// List the members of a JSON object document in source order, each value as its source text, so the last of
// several keys folding to one field wins as in Go, and an integer keeps its exact text.
function objectMembers(text: string): Array<[string, string]> {
  const members: Array<[string, string]> = [];
  const skipSpace = (index: number): number => { while (/[ \t\n\r]/.test(text[index] ?? '')) index++; return index; };
  let index = skipSpace(skipSpace(0) + 1);
  while (text[index] === '"') {
    const keyEnd = skipJSONValue(text, index);
    const valueStart = skipSpace(skipSpace(keyEnd) + 1);
    const valueEnd = skipJSONValue(text, valueStart);
    members.push([JSON.parse(text.slice(index, keyEnd)) as string, text.slice(valueStart, valueEnd)]);
    index = skipSpace(valueEnd);
    if (text[index] === ',') index = skipSpace(index + 1);
  }
  return members;
}

/**
 * Fill the cursor fields as Go's encoding/json does: keys match case-insensitively, the last duplicate wins, unknown
 * keys and null values are ignored, a value of the wrong JSON type rejects the token, and an escaped lone surrogate
 * decodes as U+FFFD.
 */
function decodeCursorPosition(token: string): InvoiceChangeCursorPosition {
  let document: unknown;
  try { document = JSON.parse(token); }
  catch { throw new InvoiceChangeCursorInvalidError(); }
  const position: InvoiceChangeCursorPosition = { realmId: '', environment: '', since: '', from: '', seen: [], offset: 0 };
  if (document === null) return position;
  if (typeof document !== 'object' || Array.isArray(document)) throw new InvoiceChangeCursorInvalidError();
  for (const [name, source] of objectMembers(token)) {
    const value: unknown = JSON.parse(source);
    if (value === null) continue;
    const folded = goFieldKey(name);
    const field = stringFields[folded];
    if (field) {
      if (typeof value !== 'string') throw new InvoiceChangeCursorInvalidError();
      position[field] = wellFormed(value);
    } else if (folded === 'seen') {
      if (!Array.isArray(value) || value.some(item => item !== null && typeof item !== 'string')) throw new InvoiceChangeCursorInvalidError();
      position.seen = value.map(item => (typeof item === 'string' ? wellFormed(item) : ''));
    } else if (folded === 'offset') {
      // Go rejects 1.0 or 1e2 for an int field; offsets past 2^53 are far beyond any real page position.
      if (!/^-?(0|[1-9][0-9]*)$/.test(source) || !Number.isSafeInteger(Number(source))) throw new InvoiceChangeCursorInvalidError();
      position.offset = Number(source);
    }
  }
  return position;
}

/** Decode and validate a cursor token; throws InvoiceChangeCursorInvalidError for any invalid token. */
export function decodeInvoiceChangeCursor(token: string): { position: InvoiceChangeCursorPosition; window: InvoiceWindow } {
  const position = decodeCursorPosition(token);
  if (!realmIDPattern.test(position.realmId)) throw new InvoiceChangeCursorInvalidError();
  if (position.offset < 0 || position.seen.length > MaximumSeenInvoiceVersions) throw new InvoiceChangeCursorInvalidError('QuickBooks invoice cursor is out of bounds');
  const window = new InvoiceWindow(zeroTime, zeroTime, new Map(), position.offset);
  if (position.since === '' && position.from === '' && position.seen.length === 0) return { position, window };
  try {
    window.since = parseRFC3339Nanoseconds(position.since);
    window.from = parseRFC3339Nanoseconds(position.from);
  } catch { throw new InvoiceChangeCursorInvalidError('QuickBooks invoice cursor has an invalid watermark'); }
  if (window.from > window.since) throw new InvoiceChangeCursorInvalidError('QuickBooks invoice cursor has an invalid watermark');
  for (const entry of position.seen) {
    const separator = entry.indexOf('/');
    const unix = separator < 0 ? '' : entry.slice(0, separator);
    const key = separator < 0 ? '' : entry.slice(separator + 1);
    const seconds = goIntegerPattern.test(unix) ? BigInt(unix) : undefined;
    const updatedAt = seconds !== undefined && seconds >= int64Minimum && seconds <= int64Maximum ? seconds * second : undefined;
    if (updatedAt === undefined || !key.includes(':') || updatedAt < window.from || updatedAt > window.since) throw new InvoiceChangeCursorInvalidError('QuickBooks invoice cursor has an invalid delivered version');
    window.seen.set(key, updatedAt);
  }
  return { position, window };
}
