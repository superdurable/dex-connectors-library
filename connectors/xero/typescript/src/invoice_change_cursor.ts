// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Cursor } from '@superdurable/connectors-sdk/pollingtrigger';

/**
 * How far before the newest delivered UpdatedDateUTC each invoiceChanged poll rereads, in milliseconds:
 * If-Modified-Since has one-second precision and a change may become visible after a later one.
 */
export const InvoiceChangeOverlapMs = 5_000;
/** The most delivered changes the invoiceChanged cursor remembers inside the overlap window, under the kit's 64 KiB. */
export const MaximumSeenInvoiceChanges = 2_000;

const int64Minimum = -(1n << 63n);
const int64Maximum = (1n << 63n) - 1n;

/** Thrown for an invoiceChanged cursor token that Go's encoding/json would not decode into a usable cursor. */
export class InvoiceChangeCursorInvalidError extends Error {
  /** Describe the rejected token without echoing it. */
  constructor(message = 'Xero invoice cursor is not decodable') { super(message); this.name = 'InvoiceChangeCursorInvalidError'; }
}

/**
 * The committed cursor in the Go struct's field order. since is the newest UpdatedDateUTC delivered, in Unix
 * milliseconds; seen lists changes already delivered inside the overlap window; page is set only while more than
 * one page of invoices changed within that window.
 */
export interface InvoiceChangeCursor { filter: string; tenantId: string; since: number; page: number; seen: string[] }

/** Return the If-Modified-Since instant in Unix milliseconds: the overlap before since, to the second. */
export function invoiceChangeWindowStart(sinceMs: number): number {
  return Math.floor((sinceMs - InvoiceChangeOverlapMs) / 1000) * 1000;
}

/** Format Unix milliseconds as Go's %013d does. */
export function paddedMilliseconds(milliseconds: number): string {
  return milliseconds < 0 ? `-${String(-milliseconds).padStart(12, '0')}` : String(milliseconds).padStart(13, '0');
}

// Go's sort.Strings compares UTF-8 bytes.
const compareUTF8 = (left: string, right: string): number => Buffer.compare(Buffer.from(left), Buffer.from(right));

/**
 * Keep keys inside the window of since, sorted by UTF-8 bytes and unique, newest MaximumSeenInvoiceChanges last. An
 * evicted change can be delivered again with its same Trigger event ID, which the target deduplicates.
 */
export function pruneSeenInvoiceChanges(keys: readonly string[], sinceMs: number): string[] {
  const floor = paddedMilliseconds(invoiceChangeWindowStart(sinceMs));
  const kept = [...new Set(keys.filter(key => compareUTF8(key, floor) >= 0))].sort(compareUTF8);
  return kept.slice(Math.max(0, kept.length - MaximumSeenInvoiceChanges));
}

// Replace each unpaired surrogate with U+FFFD, as Go's encoder and decoder do.
const wellFormed = (text: string): string => text.replace(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g, '\uFFFD');

// Escape as Go's json.Marshal does: HTML-safe, line and paragraph separators escaped, lone surrogates replaced.
function encodeGoJSONString(text: string): string {
  return JSON.stringify(wellFormed(text)).replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
}

/** Encode the cursor as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings. */
export function encodeInvoiceChangeCursor(position: InvoiceChangeCursor): Cursor {
  const members = [`"filter":${encodeGoJSONString(position.filter)}`];
  if (position.tenantId) members.push(`"tenantId":${encodeGoJSONString(position.tenantId)}`);
  members.push(`"since":${position.since}`);
  if (position.page) members.push(`"page":${position.page}`);
  if (position.seen.length) members.push(`"seen":[${position.seen.map(encodeGoJSONString).join(',')}]`);
  return { token: `{${members.join(',')}}` };
}

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

// Decode a Go int64 field: Go rejects 1.0, 1e2, and values outside int64. Values past 2^53 milliseconds, far beyond
// any Xero date, are rejected too.
function decodeInteger(source: string): number {
  if (!/^-?(0|[1-9][0-9]*)$/.test(source)) throw new InvoiceChangeCursorInvalidError();
  const exact = BigInt(source);
  if (exact < int64Minimum || exact > int64Maximum || !Number.isSafeInteger(Number(source))) throw new InvoiceChangeCursorInvalidError();
  return Number(source) || 0;
}

/**
 * Decode a token as Go's json.Unmarshal fills the cursor struct: keys match case-insensitively, the last duplicate
 * wins, unknown keys and null values are ignored, a value of the wrong JSON type rejects the token, and an escaped
 * lone surrogate decodes as U+FFFD. Throws InvoiceChangeCursorInvalidError.
 */
export function decodeInvoiceChangeCursor(token: string): InvoiceChangeCursor {
  let document: unknown;
  try { document = JSON.parse(token); }
  catch { throw new InvoiceChangeCursorInvalidError(); }
  const position: InvoiceChangeCursor = { filter: '', tenantId: '', since: 0, page: 0, seen: [] };
  if (document === null) return position;
  if (typeof document !== 'object' || Array.isArray(document)) throw new InvoiceChangeCursorInvalidError();
  for (const [name, source] of objectMembers(token)) {
    const value: unknown = JSON.parse(source);
    if (value === null) continue;
    const folded = goFieldKey(name);
    if (folded === 'filter' || folded === 'tenantid') {
      if (typeof value !== 'string') throw new InvoiceChangeCursorInvalidError();
      position[folded === 'filter' ? 'filter' : 'tenantId'] = wellFormed(value);
    } else if (folded === 'since' || folded === 'page') {
      position[folded] = decodeInteger(source);
    } else if (folded === 'seen') {
      if (!Array.isArray(value) || value.some(item => item !== null && typeof item !== 'string')) throw new InvoiceChangeCursorInvalidError();
      position.seen = value.map(item => (typeof item === 'string' ? wellFormed(item) : ''));
    }
  }
  return position;
}
