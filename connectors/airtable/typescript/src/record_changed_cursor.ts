// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import { NanosecondsPerMillisecond, NanosecondsPerSecond, formatRFC3339Nanoseconds, parseRFC3339Nanoseconds, truncateToMillisecond } from './record_changed_time.js';

/**
 * The recordChanged cursor: Go's version 2 token, decoded and encoded byte for byte. Times are bigint
 * nanoseconds since the Unix epoch and undefined is Go's zero time.
 */

const recordChangedCursorVersion = 2;
/** How far before the latest delivered change a poll still looks for late-visible changes. */
const recordChangedOverlap = 5n * NanosecondsPerSecond;
const maximumRecordChangedSeen = 2000;
const recordIdPattern = /^rec[0-9A-Za-z]{14}$/;
const int64DecimalPattern = /^[+-]?[0-9]+$/;
const maximumInt64 = (1n << 63n) - 1n;

/** A stored token that must be reseeded. */
export class RecordChangedCursorError extends Error {
  constructor(message: string) { super(message); this.name = 'RecordChangedCursorError'; }
}

/** One open listRecords iteration: its filter bound and the next page's offset. */
export interface RecordChangedScan { since: string; offset: string }

/** One delivered record and its modification time, truncated to Airtable's milliseconds. */
interface SeenRecord { recordId: string; at: bigint }

/** Return the later time; undefined, Go's zero time, is the earliest. */
export function later(left: bigint | undefined, right: bigint | undefined): bigint | undefined {
  return right !== undefined && (left === undefined || right > left) ? right : left;
}

/** The decoded cursor, mutated while one fetch delivers records. */
export class RecordChangedPosition {
  after: bigint | undefined;
  floor: bigint | undefined;
  burst: bigint | undefined;
  seen: SeenRecord[] = [];
  scan: RecordChangedScan | undefined;

  constructor(readonly fieldId: string) {}

  /** Return the earliest modification time a poll still delivers; undefined means every record. */
  lowerBound(): bigint | undefined {
    return this.after === undefined ? undefined : later(this.after - recordChangedOverlap, this.floor);
  }

  /** Report a record older than the lower bound or already delivered at this time. */
  isDelivered(recordId: string, modifiedAt: bigint): boolean {
    const lowerBound = this.lowerBound();
    if (lowerBound !== undefined && modifiedAt < lowerBound) return true;
    const at = truncateToMillisecond(modifiedAt);
    return this.seen.some(seen => seen.recordId === recordId && seen.at === at);
  }

  /** Remember one delivered record; records arrive oldest first within a scan. */
  deliver(recordId: string, modifiedAt: bigint): void {
    if (this.burst !== undefined && modifiedAt > this.burst) this.closeBurst();
    this.after = later(this.after, modifiedAt);
    this.seen.push({ recordId, at: truncateToMillisecond(modifiedAt) });
    this.bound();
  }

  /** Move the floor past a burst time once every record at that time was listed. */
  closeBurst(): void {
    if (this.burst !== undefined) {
      this.floor = later(this.floor, this.burst + NanosecondsPerMillisecond);
      this.burst = undefined;
    }
    this.bound();
  }

  /** Return the listRecords filter bound: the lower bound minus one second of formula rounding. */
  filterSince(): bigint {
    const lowerBound = this.lowerBound();
    return lowerBound === undefined ? 0n : truncateToMillisecond(lowerBound - NanosecondsPerSecond);
  }

  private bound(): void {
    const lowerBound = this.lowerBound();
    if (lowerBound !== undefined) this.seen = this.seen.filter(seen => !(seen.at < lowerBound));
    if (this.seen.length <= maximumRecordChangedSeen) return;
    // Array.prototype.sort is stable, as Go's slices.SortStableFunc.
    this.seen.sort((left, right) => (left.at < right.at ? -1 : left.at > right.at ? 1 : 0));
    const dropped = this.seen[this.seen.length - maximumRecordChangedSeen - 1]!.at;
    this.seen = this.seen.filter(seen => seen.at > dropped);
    if (this.after !== undefined && dropped < truncateToMillisecond(this.after)) {
      // The scan, oldest first, already listed every record at the dropped time.
      this.floor = later(this.floor, dropped + NanosecondsPerMillisecond);
      return;
    }
    this.burst = later(this.burst, dropped);
  }
}

/** Encode the token as Go's json.Marshal of recordChangedCursorToken. */
export function encodeRecordChangedCursor(position: RecordChangedPosition): Cursor {
  const parts = [`"v":${recordChangedCursorVersion}`, `"fieldId":${encodeGoJSONValue(position.fieldId)}`, `"after":${encodeGoJSONValue(formatCursorTime(position.after))}`];
  if (position.floor !== undefined) parts.push(`"floor":${encodeGoJSONValue(formatCursorTime(position.floor))}`);
  if (position.burst !== undefined) parts.push(`"burst":${encodeGoJSONValue(formatCursorTime(position.burst))}`);
  if (position.seen.length > 0) {
    const after = truncateToMillisecond(position.after ?? 0n);
    parts.push(`"seen":[${position.seen.map(seen => encodeGoJSONValue(`${seen.recordId}:${(after - seen.at) / NanosecondsPerMillisecond}`)).join(',')}]`);
  }
  if (position.scan) parts.push(`"scan":{"since":${encodeGoJSONValue(position.scan.since)},"offset":${encodeGoJSONValue(position.scan.offset)}}`);
  return { token: `{${parts.join(',')}}` };
}

/** Validate a stored token as Go's decodeRecordChangedCursor does; throws RecordChangedCursorError when it must be reseeded. */
export function decodeRecordChangedCursor(encoded: string): RecordChangedPosition {
  let token: DecodedToken;
  try { token = decodeToken(encoded); }
  catch { throw new RecordChangedCursorError('Airtable recordChanged cursor is not a version 2 cursor'); }
  if (token.version !== BigInt(recordChangedCursorVersion)) throw new RecordChangedCursorError('Airtable recordChanged cursor is not a version 2 cursor');
  if (token.seen.length > maximumRecordChangedSeen) throw new RecordChangedCursorError('Airtable recordChanged cursor holds too many record IDs');
  const position = new RecordChangedPosition(token.fieldId);
  position.scan = token.scan;
  position.after = parseCursorTime(token.after);
  position.floor = parseCursorTime(token.floor);
  position.burst = parseCursorTime(token.burst);
  if (position.after === undefined && (token.seen.length > 0 || position.floor !== undefined || position.burst !== undefined)) {
    throw new RecordChangedCursorError('Airtable recordChanged cursor remembers records without a time');
  }
  if (token.scan && (!token.scan.since || !token.scan.offset || parseRFC3339Nanoseconds(token.scan.since) === undefined)) {
    throw new RecordChangedCursorError('Airtable recordChanged cursor scan is invalid');
  }
  const after = truncateToMillisecond(position.after ?? 0n);
  for (const entry of token.seen) {
    const separator = entry.indexOf(':');
    const recordId = separator < 0 ? entry : entry.slice(0, separator), delta = separator < 0 ? '' : entry.slice(separator + 1);
    const milliseconds = int64DecimalPattern.test(delta) ? BigInt(delta) : -1n;
    if (separator < 0 || milliseconds < 0n || milliseconds > maximumInt64 || !recordIdPattern.test(recordId)) {
      throw new RecordChangedCursorError('Airtable recordChanged cursor holds an invalid record entry');
    }
    position.seen.push({ recordId, at: after - milliseconds * NanosecondsPerMillisecond });
  }
  return position;
}

function parseCursorTime(text: string): bigint | undefined {
  if (text === '') return undefined;
  const at = parseRFC3339Nanoseconds(text);
  if (at === undefined) throw new RecordChangedCursorError('Airtable recordChanged cursor time is invalid');
  return at;
}

function formatCursorTime(at: bigint | undefined): string { return at === undefined ? '' : formatRFC3339Nanoseconds(at); }

interface DecodedToken { version: bigint; fieldId: string; after: string; floor: string; burst: string; seen: string[]; scan: RecordChangedScan | undefined }

/** A parsed JSON value that keeps object pairs in document order and number literals as written. */
type ParsedJSON = null | boolean | string | { number: string } | ParsedJSON[] | { pairs: Array<[string, ParsedJSON]> };

const tokenFields = ['v', 'fieldId', 'after', 'floor', 'burst', 'seen', 'scan'] as const;

// Go's json.Unmarshal: keys match fields ignoring case, later keys win, and unknown keys are ignored.
function decodeToken(encoded: string): DecodedToken {
  const document = new StrictJSONParser(encoded).parseDocument();
  const token: DecodedToken = { version: 0n, fieldId: '', after: '', floor: '', burst: '', seen: [], scan: undefined };
  if (document === null) return token;
  for (const [name, value] of objectPairs(document, tokenFields)) {
    if (name === 'v') {
      if (value === null) continue;
      if (typeof value !== 'object' || !('number' in value) || !/^-?(0|[1-9][0-9]*)$/.test(value.number)) throw new TypeError('v is not an int');
      token.version = BigInt(value.number);
      if (token.version > maximumInt64 || token.version < -maximumInt64 - 1n) throw new TypeError('v overflows int');
    } else if (name === 'seen') {
      if (value === null) { token.seen = []; continue; }
      if (!Array.isArray(value)) throw new TypeError('seen is not an array');
      token.seen = value.map(item => goString(item, ''));
    } else if (name === 'scan') {
      if (value === null) { token.scan = undefined; continue; }
      const scan = { since: '', offset: '' };
      for (const [scanName, scanValue] of objectPairs(value, ['since', 'offset'] as const)) scan[scanName] = goString(scanValue, scan[scanName]);
      token.scan = scan;
    } else {
      token[name] = goString(value, token[name]);
    }
  }
  return token;
}

function objectPairs<Name extends string>(value: ParsedJSON, names: readonly Name[]): Array<[Name, ParsedJSON]> {
  if (value === null || typeof value !== 'object' || !('pairs' in value)) throw new TypeError('JSON value is not an object');
  const matched: Array<[Name, ParsedJSON]> = [];
  for (const [key, item] of value.pairs) {
    const name = names.find(candidate => candidate === key) ?? names.find(candidate => candidate.toLowerCase() === key.toLowerCase());
    if (name !== undefined) matched.push([name, item]);
  }
  return matched;
}

function goString(value: ParsedJSON, current: string): string {
  if (value === null) return current;
  if (typeof value !== 'string') throw new TypeError('JSON value is not a string');
  return value;
}

const jsonNumberPattern = /-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?/y;
const jsonStringPattern = /"(?:[^"\\]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"/y;

/** A strict RFC 8259 parser, as Go's decoder accepts, nested at most 10000 levels. */
class StrictJSONParser {
  private index = 0;
  constructor(private readonly text: string) {}

  parseDocument(): ParsedJSON {
    const value = this.parseValue(0);
    this.skipWhitespace();
    if (this.index !== this.text.length) throw new SyntaxError('trailing data after JSON value');
    return value;
  }

  private parseValue(depth: number): ParsedJSON {
    if (depth > 10_000) throw new SyntaxError('JSON nests too deeply');
    this.skipWhitespace();
    const character = this.text[this.index];
    if (character === '{') return this.parseObject(depth);
    if (character === '[') return this.parseArray(depth);
    if (character === '"') return this.parseString();
    for (const [literal, value] of [['true', true], ['false', false], ['null', null]] as const) {
      if (this.text.startsWith(literal, this.index)) { this.index += literal.length; return value; }
    }
    const number = this.matchAt(jsonNumberPattern);
    if (!number) throw new SyntaxError('invalid JSON value');
    this.index += number[0].length;
    return { number: number[0] };
  }

  private parseObject(depth: number): ParsedJSON {
    const pairs: Array<[string, ParsedJSON]> = [];
    this.index++;
    this.skipWhitespace();
    if (this.text[this.index] === '}') { this.index++; return { pairs }; }
    for (;;) {
      this.skipWhitespace();
      if (this.text[this.index] !== '"') throw new SyntaxError('object key must be a string');
      const key = this.parseString();
      this.skipWhitespace();
      if (this.text[this.index++] !== ':') throw new SyntaxError('expected colon');
      pairs.push([key, this.parseValue(depth + 1)]);
      this.skipWhitespace();
      const separator = this.text[this.index++];
      if (separator === '}') return { pairs };
      if (separator !== ',') throw new SyntaxError('expected comma or closing brace');
    }
  }

  private parseArray(depth: number): ParsedJSON {
    const items: ParsedJSON[] = [];
    this.index++;
    this.skipWhitespace();
    if (this.text[this.index] === ']') { this.index++; return items; }
    for (;;) {
      items.push(this.parseValue(depth + 1));
      this.skipWhitespace();
      const separator = this.text[this.index++];
      if (separator === ']') return items;
      if (separator !== ',') throw new SyntaxError('expected comma or closing bracket');
    }
  }

  private parseString(): string {
    const match = this.matchAt(jsonStringPattern);
    // JSON forbids raw control characters inside a string.
    if (!match || [...match[0]].some(character => character < ' ')) throw new SyntaxError('invalid JSON string');
    this.index += match[0].length;
    return JSON.parse(match[0]) as string;
  }

  private matchAt(pattern: RegExp): RegExpExecArray | null {
    pattern.lastIndex = this.index;
    return pattern.exec(this.text);
  }

  private skipWhitespace(): void {
    while (this.index < this.text.length && ' \t\n\r'.includes(this.text[this.index]!)) this.index++;
  }
}
