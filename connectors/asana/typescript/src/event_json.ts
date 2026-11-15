// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * Strict JSON scanning that keeps each value's raw bytes, as Go's json.RawMessage does. Event IDs hash
 * json.Compact of Asana change values, which keeps string escapes and number literals exactly as sent.
 */

/** The bytes are not one strict JSON value, or a value has the wrong JSON type for its Go field. */
export class InvalidEventJSONError extends Error {
  /** Describe the problem without echoing provider text. */
  constructor(message: string) { super(message); this.name = 'InvalidEventJSONError'; }
}

/** The JSON kind of one scanned value. */
export type RawJSONKind = 'object' | 'array' | 'string' | 'number' | 'true' | 'false' | 'null';

/** One JSON value with its exact source bytes. */
export class RawJSONValue {
  /**
   * @param raw The value's bytes, including insignificant whitespace inside containers.
   * @param kind The JSON kind.
   * @param members An object's members in source order; duplicate keys are kept.
   * @param elements An array's elements.
   * @param text A string's decoded text, with invalid UTF-8 replaced as Go does.
   */
  constructor(
    readonly raw: Uint8Array,
    readonly kind: RawJSONKind,
    readonly members: ReadonlyArray<readonly [string, RawJSONValue]> = [],
    readonly elements: readonly RawJSONValue[] = [],
    readonly text = '',
  ) {}

  /** Return the last member whose key matches name, case-insensitively as Go's decoder does. */
  member(name: string): RawJSONValue | undefined {
    const folded = name.toLowerCase();
    let found: RawJSONValue | undefined;
    for (const [key, value] of this.members) if (key === name || key.toLowerCase() === folded) found = value;
    return found;
  }

  /** Return a member decoded into a Go pointer to struct: undefined when absent or null. */
  optionalObject(name: string): RawJSONValue | undefined {
    const value = this.member(name);
    if (!value || value.kind === 'null') return undefined;
    if (value.kind !== 'object') throw new InvalidEventJSONError(`${name} must be an object`);
    return value;
  }

  /** Return a member decoded into a Go string: empty when absent or null. */
  stringMember(name: string): string {
    const value = this.member(name);
    if (!value || value.kind === 'null') return '';
    if (value.kind !== 'string') throw new InvalidEventJSONError(`${name} must be a string`);
    return value.text;
  }

  /** Return a member decoded into a Go bool: false when absent or null. */
  booleanMember(name: string): boolean {
    const value = this.member(name);
    if (!value || value.kind === 'null') return false;
    if (value.kind !== 'true' && value.kind !== 'false') throw new InvalidEventJSONError(`${name} must be a boolean`);
    return value.kind === 'true';
  }

  /** Return a member decoded into a Go slice: empty when absent or null. */
  arrayMember(name: string): readonly RawJSONValue[] {
    const value = this.member(name);
    if (!value || value.kind === 'null') return [];
    if (value.kind !== 'array') throw new InvalidEventJSONError(`${name} must be an array`);
    return value.elements;
  }
}

const quote = 0x22, backslash = 0x5c, comma = 0x2c, colon = 0x3a;
const openBrace = 0x7b, closeBrace = 0x7d, openBracket = 0x5b, closeBracket = 0x5d;
const numberPattern = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/;
const escapePattern = /^\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4})/;
const utf8 = new TextDecoder('utf-8');
const latin1 = new TextDecoder('latin1');

/**
 * Scan exactly one JSON value surrounded only by whitespace. Throws InvalidEventJSONError for any syntax Go's
 * encoding/json rejects, such as NaN, a trailing comma, or trailing data.
 */
export function parseRawJSON(raw: Uint8Array): RawJSONValue {
  const scanner = new RawJSONScanner(raw);
  const value = scanner.scanValue(scanner.skipWhitespace(0));
  if (scanner.skipWhitespace(value.end) !== raw.length) throw new InvalidEventJSONError('JSON has trailing data');
  return value.value;
}

/** Remove insignificant whitespace and keep every other byte, as Go's json.Compact does. */
export function compactRawJSON(raw: Uint8Array): Uint8Array {
  const compacted: number[] = [];
  let isInString = false;
  for (let index = 0; index < raw.length; index++) {
    const byte = raw[index]!;
    if (isInString) {
      compacted.push(byte);
      if (byte === backslash) compacted.push(raw[++index]!);
      else if (byte === quote) isInString = false;
    } else if (!isWhitespace(byte)) {
      compacted.push(byte);
      isInString = byte === quote;
    }
  }
  return Uint8Array.from(compacted);
}

function isWhitespace(byte: number): boolean { return byte === 0x20 || byte === 0x09 || byte === 0x0a || byte === 0x0d; }

interface Scanned { value: RawJSONValue; end: number }

class RawJSONScanner {
  private depth = 0;

  constructor(private readonly raw: Uint8Array) {}

  skipWhitespace(index: number): number {
    while (index < this.raw.length && isWhitespace(this.raw[index]!)) index++;
    return index;
  }

  scanValue(start: number): Scanned {
    if (start >= this.raw.length) throw new InvalidEventJSONError('JSON ends before a value');
    const lead = this.raw[start]!;
    if (lead === openBrace || lead === openBracket) {
      // Go's decoder accepts 10000 nesting levels.
      if (++this.depth > 10_000) throw new InvalidEventJSONError('JSON nests too deeply');
      try { return lead === openBrace ? this.scanObject(start) : this.scanArray(start); } finally { this.depth--; }
    }
    if (lead === quote) {
      const [end, text] = this.scanString(start);
      return { value: new RawJSONValue(this.raw.subarray(start, end), 'string', [], [], text), end };
    }
    for (const literal of ['true', 'false', 'null'] as const) {
      if (latin1.decode(this.raw.subarray(start, start + literal.length)) === literal) {
        const end = start + literal.length;
        return { value: new RawJSONValue(this.raw.subarray(start, end), literal), end };
      }
    }
    const number = numberPattern.exec(latin1.decode(this.raw.subarray(start, start + 1024)));
    if (!number || number[0].length === 1024) throw new InvalidEventJSONError('JSON has an invalid value');
    const end = start + number[0].length;
    return { value: new RawJSONValue(this.raw.subarray(start, end), 'number'), end };
  }

  private scanObject(start: number): Scanned {
    const members: Array<[string, RawJSONValue]> = [];
    let index = this.skipWhitespace(start + 1);
    if (this.raw[index] === closeBrace) return { value: new RawJSONValue(this.raw.subarray(start, index + 1), 'object'), end: index + 1 };
    for (;;) {
      if (this.raw[index] !== quote) throw new InvalidEventJSONError('JSON object key must be a string');
      const [keyEnd, key] = this.scanString(index);
      index = this.skipWhitespace(keyEnd);
      if (this.raw[index] !== colon) throw new InvalidEventJSONError('JSON object key must be followed by a colon');
      const member = this.scanValue(this.skipWhitespace(index + 1));
      members.push([key, member.value]);
      index = this.skipWhitespace(member.end);
      if (this.raw[index] === comma) { index = this.skipWhitespace(index + 1); continue; }
      if (this.raw[index] === closeBrace) return { value: new RawJSONValue(this.raw.subarray(start, index + 1), 'object', members), end: index + 1 };
      throw new InvalidEventJSONError('JSON object is not closed');
    }
  }

  private scanArray(start: number): Scanned {
    const elements: RawJSONValue[] = [];
    let index = this.skipWhitespace(start + 1);
    if (this.raw[index] === closeBracket) return { value: new RawJSONValue(this.raw.subarray(start, index + 1), 'array'), end: index + 1 };
    for (;;) {
      const element = this.scanValue(index);
      elements.push(element.value);
      index = this.skipWhitespace(element.end);
      if (this.raw[index] === comma) { index = this.skipWhitespace(index + 1); continue; }
      if (this.raw[index] === closeBracket) return { value: new RawJSONValue(this.raw.subarray(start, index + 1), 'array', [], elements), end: index + 1 };
      throw new InvalidEventJSONError('JSON array is not closed');
    }
  }

  private scanString(start: number): [number, string] {
    let index = start + 1;
    while (index < this.raw.length) {
      const byte = this.raw[index]!;
      if (byte === quote) return [index + 1, JSON.parse(utf8.decode(this.raw.subarray(start, index + 1))) as string];
      if (byte < 0x20) throw new InvalidEventJSONError('JSON string has a control character');
      if (byte === backslash) {
        const escape = escapePattern.exec(latin1.decode(this.raw.subarray(index, index + 6)));
        if (!escape) throw new InvalidEventJSONError('JSON string has an invalid escape');
        index += escape[0].length;
        continue;
      }
      index++;
    }
    throw new InvalidEventJSONError('JSON string is not closed');
  }
}
