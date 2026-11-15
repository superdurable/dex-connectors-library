// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// A JSON reader that keeps what Go's encoding/json can see and JSON.parse loses: object members in document
// order with duplicates, and each number's literal text, so 9605.0 or a 20-digit ID is refused as Go refuses it.

/** One parsed JSON value. */
export type GoJSONValue =
  | { kind: 'object'; members: GoJSONMember[] }
  | { kind: 'array'; elements: GoJSONValue[] }
  | { kind: 'string'; text: string }
  | { kind: 'number'; literal: string }
  | { kind: 'boolean'; value: boolean }
  | { kind: 'null' };

/** One object member, in document order. */
export interface GoJSONMember { key: string; value: GoJSONValue }

/** Thrown for text that is not one complete JSON value. */
export class GoJSONSyntaxError extends Error { constructor() { super('JSON is invalid'); this.name = 'GoJSONSyntaxError'; } }

/** Thrown where Go's json.Unmarshal reports a type error for a value. */
export class GoJSONTypeError extends Error { constructor(message: string) { super(message); this.name = 'GoJSONTypeError'; } }

// Go's encoding/json nesting limit.
const maximumNestingDepth = 10_000;
const int64Minimum = -(1n << 63n);
const int64Maximum = (1n << 63n) - 1n;
const numberPattern = /-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/y;
// eslint-disable-next-line no-control-regex -- JSON forbids raw control characters in strings.
const stringPattern = /"(?:[^"\\\u0000-\u001f]|\\["\\/bfnrt]|\\u[0-9a-fA-F]{4})*"/y;

/** Parse text as one JSON value; throws GoJSONSyntaxError for anything Go's scanner rejects. */
export function parseGoJSON(text: string): GoJSONValue {
  const reader = { text, index: 0 };
  const value = readValue(reader, 0);
  skipWhitespace(reader);
  if (reader.index !== text.length) throw new GoJSONSyntaxError();
  return value;
}

/**
 * Return the members Go's json.Unmarshal assigns to the named struct fields, in document order: a key matches
 * its field exactly or case-insensitively, and unknown keys are skipped.
 */
export function goFieldMembers(members: readonly GoJSONMember[], fields: readonly string[]): { field: string; value: GoJSONValue }[] {
  const folded = new Map(fields.map(field => [field.toLowerCase(), field]));
  const assigned: { field: string; value: GoJSONValue }[] = [];
  for (const member of members) {
    const field = fields.includes(member.key) ? member.key : folded.get(member.key.toLowerCase().replaceAll('ſ', 's'));
    if (field !== undefined) assigned.push({ field, value: member.value });
  }
  return assigned;
}

/** Decode a value into a Go int64 field; throws GoJSONTypeError for a fraction, an exponent, overflow, or another kind. */
export function decodeGoInt64(value: GoJSONValue): number {
  if (value.kind !== 'number' || !/^-?[0-9]+$/.test(value.literal)) throw new GoJSONTypeError('integer field has another type');
  const integer = BigInt(value.literal);
  if (integer < int64Minimum || integer > int64Maximum) throw new GoJSONTypeError('integer field overflows int64');
  if (integer > BigInt(Number.MAX_SAFE_INTEGER) || integer < BigInt(Number.MIN_SAFE_INTEGER)) throw new GoJSONTypeError('integer field exceeds the JavaScript safe range');
  return Number(integer);
}

/** Decode a value into a Go string field; throws GoJSONTypeError for another kind. */
export function decodeGoString(value: GoJSONValue): string {
  if (value.kind !== 'string') throw new GoJSONTypeError('string field has another type');
  return value.text;
}

interface Reader { text: string; index: number }

function skipWhitespace(reader: Reader): void {
  while (reader.index < reader.text.length && ' \t\n\r'.includes(reader.text[reader.index]!)) reader.index++;
}

function readValue(reader: Reader, depth: number): GoJSONValue {
  skipWhitespace(reader);
  const character = reader.text[reader.index];
  if (character === '{' || character === '[') {
    if (depth >= maximumNestingDepth) throw new GoJSONSyntaxError();
    return character === '{' ? readObject(reader, depth + 1) : readArray(reader, depth + 1);
  }
  if (character === '"') return { kind: 'string', text: readString(reader) };
  for (const [literal, value] of [['true', { kind: 'boolean', value: true }], ['false', { kind: 'boolean', value: false }], ['null', { kind: 'null' }]] as const) {
    if (reader.text.startsWith(literal, reader.index)) { reader.index += literal.length; return value; }
  }
  numberPattern.lastIndex = reader.index;
  const match = numberPattern.exec(reader.text);
  if (!match) throw new GoJSONSyntaxError();
  reader.index += match[0].length;
  return { kind: 'number', literal: match[0] };
}

function readObject(reader: Reader, depth: number): GoJSONValue {
  reader.index++;
  const members: GoJSONMember[] = [];
  skipWhitespace(reader);
  if (reader.text[reader.index] === '}') { reader.index++; return { kind: 'object', members }; }
  for (;;) {
    skipWhitespace(reader);
    const key = readString(reader);
    skipWhitespace(reader);
    if (reader.text[reader.index] !== ':') throw new GoJSONSyntaxError();
    reader.index++;
    members.push({ key, value: readValue(reader, depth) });
    skipWhitespace(reader);
    const separator = reader.text[reader.index++];
    if (separator === '}') return { kind: 'object', members };
    if (separator !== ',') throw new GoJSONSyntaxError();
  }
}

function readArray(reader: Reader, depth: number): GoJSONValue {
  reader.index++;
  const elements: GoJSONValue[] = [];
  skipWhitespace(reader);
  if (reader.text[reader.index] === ']') { reader.index++; return { kind: 'array', elements }; }
  for (;;) {
    elements.push(readValue(reader, depth));
    skipWhitespace(reader);
    const separator = reader.text[reader.index++];
    if (separator === ']') return { kind: 'array', elements };
    if (separator !== ',') throw new GoJSONSyntaxError();
  }
}

function readString(reader: Reader): string {
  stringPattern.lastIndex = reader.index;
  const match = stringPattern.exec(reader.text);
  if (!match) throw new GoJSONSyntaxError();
  reader.index += match[0].length;
  // Go decodes an unpaired surrogate escape as U+FFFD.
  return (JSON.parse(match[0]) as string & { toWellFormed(): string }).toWellFormed();
}
