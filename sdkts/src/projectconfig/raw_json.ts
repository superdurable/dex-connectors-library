// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** Thrown for bytes that are not one complete JSON value, or a value the Go-compatible encoder cannot write. */
export class InvalidRawJSONError extends Error { constructor(message = 'raw JSON is invalid') { super(message); this.name = 'InvalidRawJSONError'; } }

/** Go's encoding/json nesting limit; deeper values are invalid in both SDKs. */
const maxNestingDepth = 10_000;

const quote = 0x22, backslash = 0x5c, comma = 0x2c, colon = 0x3a, minus = 0x2d, plus = 0x2b, period = 0x2e;
const openBrace = 0x7b, closeBrace = 0x7d, openBracket = 0x5b, closeBracket = 0x5d;

/** The kind of a JSON value, from its first byte. */
export type RawJSONKind = 'object' | 'array' | 'string' | 'number' | 'true' | 'false' | 'null';

/** One member of a JSON object: its key decoded as Go decodes strings, and the raw bytes of its value. */
export interface RawJSONMember { key: string; value: Buffer }

/**
 * Validate one JSON value as Go's scanner does and return it compacted as Go writes a json.RawMessage:
 * whitespace outside strings removed and, inside strings, <, >, &, U+2028, and U+2029 escaped. Every other
 * byte stays as stored, including numbers of any length and bytes that are not valid UTF-8.
 */
export function compactGoRawJSON(raw: Uint8Array): Buffer {
  const bytes = Buffer.from(raw.buffer, raw.byteOffset, raw.byteLength);
  const start = skipWhitespace(bytes, 0);
  const end = scanValue(bytes, start);
  if (skipWhitespace(bytes, end) !== bytes.length) throw new InvalidRawJSONError();
  const output: number[] = [];
  let isInString = false;
  for (let index = start; index < end; index++) {
    const byte = bytes[index]!;
    if (!isInString) {
      if (byte === 0x20 || byte === 0x09 || byte === 0x0a || byte === 0x0d) continue;
      if (byte === quote) isInString = true;
      output.push(byte);
      continue;
    }
    if (byte === backslash) { output.push(byte, bytes[++index]!); continue; }
    if (byte === quote) isInString = false;
    if (byte === 0x3c || byte === 0x3e || byte === 0x26) { pushAscii(output, `\\u00${byte.toString(16)}`); continue; }
    if (byte === 0xe2 && bytes[index + 1] === 0x80 && (bytes[index + 2] === 0xa8 || bytes[index + 2] === 0xa9)) {
      pushAscii(output, bytes[index + 2] === 0xa8 ? '\\u2028' : '\\u2029');
      index += 2;
      continue;
    }
    output.push(byte);
  }
  return Buffer.from(output);
}

/** Report the kind of one complete JSON value; throws for invalid JSON. */
export function rawJSONKind(raw: Uint8Array): RawJSONKind {
  const bytes = Buffer.from(raw.buffer, raw.byteOffset, raw.byteLength);
  const start = skipWhitespace(bytes, 0);
  if (skipWhitespace(bytes, scanValue(bytes, start)) !== bytes.length) throw new InvalidRawJSONError();
  const first = bytes[start];
  if (first === openBrace) return 'object';
  if (first === openBracket) return 'array';
  if (first === quote) return 'string';
  if (first === 0x74) return 'true';
  if (first === 0x66) return 'false';
  if (first === 0x6e) return 'null';
  return 'number';
}

/** Split one JSON object into members in document order, keeping each value's raw bytes; duplicates are kept. */
export function rawJSONObjectMembers(raw: Uint8Array): RawJSONMember[] {
  const bytes = Buffer.from(raw.buffer, raw.byteOffset, raw.byteLength);
  let index = skipWhitespace(bytes, 0);
  if (bytes[index] !== openBrace) throw new InvalidRawJSONError();
  const members: RawJSONMember[] = [];
  index = skipWhitespace(bytes, index + 1);
  if (bytes[index] === closeBrace) return finishContainer(bytes, index, members);
  for (;;) {
    const keyEnd = scanString(bytes, index);
    const key = decodeGoJSONString(bytes.subarray(index, keyEnd));
    index = skipWhitespace(bytes, keyEnd);
    if (bytes[index] !== colon) throw new InvalidRawJSONError();
    const valueStart = skipWhitespace(bytes, index + 1);
    const valueEnd = scanValue(bytes, valueStart);
    members.push({ key, value: bytes.subarray(valueStart, valueEnd) });
    index = skipWhitespace(bytes, valueEnd);
    if (bytes[index] === closeBrace) return finishContainer(bytes, index, members);
    if (bytes[index] !== comma) throw new InvalidRawJSONError();
    index = skipWhitespace(bytes, index + 1);
  }
}

/** Split one JSON array into the raw bytes of its elements. */
export function rawJSONArrayElements(raw: Uint8Array): Buffer[] {
  const bytes = Buffer.from(raw.buffer, raw.byteOffset, raw.byteLength);
  let index = skipWhitespace(bytes, 0);
  if (bytes[index] !== openBracket) throw new InvalidRawJSONError();
  const elements: Buffer[] = [];
  index = skipWhitespace(bytes, index + 1);
  if (bytes[index] === closeBracket) return finishContainer(bytes, index, elements);
  for (;;) {
    const end = scanValue(bytes, index);
    elements.push(bytes.subarray(index, end));
    index = skipWhitespace(bytes, end);
    if (bytes[index] === closeBracket) return finishContainer(bytes, index, elements);
    if (bytes[index] !== comma) throw new InvalidRawJSONError();
    index = skipWhitespace(bytes, index + 1);
  }
}

/**
 * Decode one JSON string, quotes included, as Go's encoding/json does: a byte sequence that is not valid
 * UTF-8 and an unpaired surrogate escape each become U+FFFD.
 */
export function decodeGoJSONString(raw: Uint8Array): string {
  const bytes = Buffer.from(raw.buffer, raw.byteOffset, raw.byteLength);
  if (bytes[0] !== quote || scanString(bytes, 0) !== bytes.length) throw new InvalidRawJSONError();
  const codePoints: number[] = [];
  for (let index = 1; index < bytes.length - 1;) {
    const byte = bytes[index]!;
    if (byte === backslash) {
      const escape = bytes[index + 1]!;
      if (escape !== 0x75) { codePoints.push(simpleEscapes[escape]!); index += 2; continue; }
      let unit = hexUnit(bytes, index + 2);
      index += 6;
      if (unit >= 0xd800 && unit <= 0xdfff) {
        const low = bytes[index] === backslash && bytes[index + 1] === 0x75 ? hexUnit(bytes, index + 2) : -1;
        if (unit <= 0xdbff && low >= 0xdc00 && low <= 0xdfff) { unit = 0x10000 + ((unit - 0xd800) << 10) + (low - 0xdc00); index += 6; }
        else unit = 0xfffd;
      }
      codePoints.push(unit);
      continue;
    }
    const [codePoint, size] = decodeGoRune(bytes, index);
    codePoints.push(codePoint);
    index += size;
  }
  let text = '';
  for (let offset = 0; offset < codePoints.length; offset += 4096) text += String.fromCodePoint(...codePoints.slice(offset, offset + 4096));
  return text;
}

/**
 * Parse a Trigger payload's raw JSON bytes, or text encoded as UTF-8, as Go's encoding/json decodes them into any. Every finite number
 * becomes the nearest double, so an integer beyond 2^53 such as 9007199254740993 becomes 9007199254740992,
 * and a value too small for a double such as 1e-400 becomes 0; a number beyond the double range, such as
 * 1e400, is refused, as Go refuses it, instead of becoming Infinity. Strings and object keys decode with Go's
 * rules: an unpaired surrogate escape and each byte of an invalid UTF-8 sequence become U+FFFD. A connector
 * that needs exact large integers or the original bytes passes its own decodePayload.
 */
export function parseTriggerPayloadJSON(raw: Uint8Array | string): unknown {
  const bytes = typeof raw === 'string' ? Buffer.from(raw, 'utf8') : Buffer.from(raw.buffer, raw.byteOffset, raw.byteLength);
  const start = skipWhitespace(bytes, 0);
  const end = scanValue(bytes, start, literal => {
    if (!Number.isFinite(Number(literal))) throw new InvalidRawJSONError('JSON number is outside the double range');
  });
  if (skipWhitespace(bytes, end) !== bytes.length) throw new InvalidRawJSONError();
  // Re-encode every string as Go decodes it, so JSON.parse yields Go's strings; the other bytes are ASCII.
  const parts: string[] = [];
  let copied = start;
  for (let index = start; index < end;) {
    if (bytes[index] !== quote) { index++; continue; }
    const stringEnd = scanString(bytes, index);
    parts.push(bytes.toString('latin1', copied, index), JSON.stringify(decodeGoJSONString(bytes.subarray(index, stringEnd))));
    index = copied = stringEnd;
  }
  parts.push(bytes.toString('latin1', copied, end));
  return JSON.parse(parts.join('')) as unknown;
}

/**
 * Encode a value as Go's encoding/json encodes the equivalent Go map, slice, string, bool, float64, or
 * json.Number: compact, HTML-safe, and with object keys sorted by their UTF-8 bytes as Go sorts map keys.
 * Go structs keep their field declaration order instead, so a Go struct payload and its TypeScript encoding
 * differ only in key order and decode alike. A Date encodes as RFC 3339 like time.Time, a bigint as an exact
 * integer, an undefined object property is omitted, and an object's toJSON result is encoded once. NaN,
 * Infinity, undefined values elsewhere, array holes, functions, symbols, Map, Set, cycles (including a
 * toJSON that returns its own object), and nesting deeper than Go's 10000 levels are refused. It encodes
 * without recursion, so the depth limit, not the call stack, bounds it.
 */
export function encodeGoJSONValue(value: unknown): string {
  if (value === undefined) throw new InvalidRawJSONError('JSON cannot encode undefined');
  const parts: string[] = [];
  const frames: EncodingFrame[] = [];
  const ancestors = new Set<object>();
  const write = (current: unknown): void => {
    if (current === null) { parts.push('null'); return; }
    if (current instanceof Date) { parts.push(encodeGoString(formatGoTime(current))); return; }
    let resolved: unknown = current;
    if (typeof current === 'object' && typeof (current as { toJSON?: unknown }).toJSON === 'function') {
      resolved = (current as { toJSON(): unknown }).toJSON();
      if (resolved === current || (typeof resolved === 'object' && resolved !== null && ancestors.has(resolved))) throw new InvalidRawJSONError('JSON cannot encode a cycle');
      if (resolved === null) { parts.push('null'); return; }
      if (resolved instanceof Date) { parts.push(encodeGoString(formatGoTime(resolved))); return; }
    }
    switch (typeof resolved) {
      case 'boolean': parts.push(resolved ? 'true' : 'false'); return;
      case 'string': parts.push(encodeGoString(resolved)); return;
      case 'bigint': parts.push(resolved.toString()); return;
      case 'number':
        if (!Number.isFinite(resolved)) throw new InvalidRawJSONError('JSON cannot encode NaN or Infinity');
        parts.push(Object.is(resolved, -0) ? '-0' : String(resolved));
        return;
      case 'object': break;
      default: throw new InvalidRawJSONError(`JSON cannot encode ${typeof resolved === 'undefined' ? 'undefined' : `a ${typeof resolved}`}`);
    }
    const container = resolved as object;
    if (container instanceof Map || container instanceof Set) throw new InvalidRawJSONError('JSON cannot encode a Map or Set');
    if (ancestors.has(container)) throw new InvalidRawJSONError('JSON cannot encode a cycle');
    if (frames.length >= maxNestingDepth) throw new InvalidRawJSONError('value is nested too deeply');
    ancestors.add(container);
    if (Array.isArray(container)) {
      parts.push('[');
      frames.push({ container, keys: undefined, length: container.length, index: 0 });
      return;
    }
    const record = container as Record<string, unknown>;
    const keys = Object.keys(record).filter(key => record[key] !== undefined).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
    parts.push('{');
    frames.push({ container, keys, length: keys.length, index: 0 });
  };
  write(value);
  while (frames.length > 0) {
    const frame = frames.at(-1)!;
    if (frame.index >= frame.length) {
      parts.push(frame.keys ? '}' : ']');
      ancestors.delete(frame.container);
      frames.pop();
      continue;
    }
    const index = frame.index++;
    if (index > 0) parts.push(',');
    if (!frame.keys) {
      if (!Object.hasOwn(frame.container, index)) throw new InvalidRawJSONError('JSON cannot encode an array hole');
      write((frame.container as unknown[])[index]);
      continue;
    }
    const key = frame.keys[index]!;
    parts.push(encodeGoString(key), ':');
    write((frame.container as Record<string, unknown>)[key]);
  }
  return parts.join('');
}

/** One open array or object while encoding: its keys in output order for an object, and the next position. */
interface EncodingFrame { container: object; keys: string[] | undefined; length: number; index: number }

/** Encode a string as Go does: well-formed UTF-8, unpaired surrogates as U+FFFD, and HTML-safe escapes. */
export function encodeGoString(value: string): string {
  return JSON.stringify((value as string & { toWellFormed(): string }).toWellFormed()).replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
}

/** Format a time as Go's time.Time JSON: RFC 3339 in UTC with trailing fractional zeros removed. */
export function formatGoTime(time: Date): string {
  if (Number.isNaN(time.getTime())) throw new InvalidRawJSONError('JSON cannot encode an invalid time');
  return time.toISOString().replace(/\.000Z$/, 'Z').replace(/(\.\d*?)0+Z$/, '$1Z');
}

const simpleEscapes: Readonly<Record<number, number>> = { 0x22: 0x22, 0x5c: 0x5c, 0x2f: 0x2f, 0x62: 0x08, 0x66: 0x0c, 0x6e: 0x0a, 0x72: 0x0d, 0x74: 0x09 };

function pushAscii(output: number[], text: string): void { for (let index = 0; index < text.length; index++) output.push(text.charCodeAt(index)); }

function finishContainer<Result>(bytes: Buffer, closeIndex: number, result: Result): Result {
  if (skipWhitespace(bytes, closeIndex + 1) !== bytes.length) throw new InvalidRawJSONError();
  return result;
}

function skipWhitespace(bytes: Buffer, index: number): number {
  while (index < bytes.length) {
    const byte = bytes[index]!;
    if (byte !== 0x20 && byte !== 0x09 && byte !== 0x0a && byte !== 0x0d) break;
    index++;
  }
  return index;
}

/**
 * Validate one value starting at start without recursion and return the index after it. onNumber receives
 * each number literal. Containers nest at most Go's 10000 levels.
 */
function scanValue(bytes: Buffer, start: number, onNumber?: (literal: string) => void): number {
  const containers: number[] = [];
  let index = start;
  for (;;) {
    index = skipWhitespace(bytes, index);
    const byte = bytes[index];
    if (byte === openBrace || byte === openBracket) {
      if (containers.length >= maxNestingDepth) throw new InvalidRawJSONError();
      containers.push(byte);
      index = skipWhitespace(bytes, index + 1);
      const close = byte === openBrace ? closeBrace : closeBracket;
      if (bytes[index] === close) { containers.pop(); index++; }
      else if (byte === openBrace) { index = expectKey(bytes, index); continue; }
      else continue;
    } else if (byte === quote) index = scanString(bytes, index);
    else if (byte === 0x74) index = scanLiteral(bytes, index, 'true');
    else if (byte === 0x66) index = scanLiteral(bytes, index, 'false');
    else if (byte === 0x6e) index = scanLiteral(bytes, index, 'null');
    else {
      const end = scanNumber(bytes, index);
      onNumber?.(bytes.toString('latin1', index, end));
      index = end;
    }
    // A complete value ends here; close finished containers and continue with the next sibling.
    for (;;) {
      if (containers.length === 0) return index;
      index = skipWhitespace(bytes, index);
      const container = containers.at(-1)!;
      const close = container === openBrace ? closeBrace : closeBracket;
      if (bytes[index] === close) { containers.pop(); index++; continue; }
      if (bytes[index] !== comma) throw new InvalidRawJSONError();
      index = skipWhitespace(bytes, index + 1);
      if (container === openBrace) index = expectKey(bytes, index);
      break;
    }
  }
}

/** Scan an object key and its colon, returning the index of the member value. */
function expectKey(bytes: Buffer, index: number): number {
  index = skipWhitespace(bytes, scanString(bytes, index));
  if (bytes[index] !== colon) throw new InvalidRawJSONError();
  return index + 1;
}

function scanString(bytes: Buffer, index: number): number {
  if (bytes[index] !== quote) throw new InvalidRawJSONError();
  for (index++; index < bytes.length; index++) {
    const byte = bytes[index]!;
    if (byte === quote) return index + 1;
    if (byte < 0x20) throw new InvalidRawJSONError();
    if (byte !== backslash) continue;
    const escape = bytes[++index];
    if (escape === 0x75) {
      if (hexUnit(bytes, index + 1) < 0) throw new InvalidRawJSONError();
      index += 4;
    } else if (escape === undefined || simpleEscapes[escape] === undefined) throw new InvalidRawJSONError();
  }
  throw new InvalidRawJSONError();
}

function scanLiteral(bytes: Buffer, index: number, literal: string): number {
  if (bytes.toString('latin1', index, index + literal.length) !== literal) throw new InvalidRawJSONError();
  return index + literal.length;
}

function scanNumber(bytes: Buffer, index: number): number {
  const isDigit = (position: number) => { const byte = bytes[position]; return byte !== undefined && byte >= 0x30 && byte <= 0x39; };
  if (bytes[index] === minus) index++;
  if (bytes[index] === 0x30) index++;
  else if (isDigit(index)) while (isDigit(index)) index++;
  else throw new InvalidRawJSONError();
  if (bytes[index] === period) {
    index++;
    if (!isDigit(index)) throw new InvalidRawJSONError();
    while (isDigit(index)) index++;
  }
  if (bytes[index] === 0x65 || bytes[index] === 0x45) {
    index++;
    if (bytes[index] === plus || bytes[index] === minus) index++;
    if (!isDigit(index)) throw new InvalidRawJSONError();
    while (isDigit(index)) index++;
  }
  return index;
}

/** Read four hexadecimal digits at index, or -1. */
function hexUnit(bytes: Buffer, index: number): number {
  const digits = bytes.toString('latin1', index, index + 4);
  return /^[0-9a-fA-F]{4}$/.test(digits) ? Number.parseInt(digits, 16) : -1;
}

/** Decode one UTF-8 sequence as Go's utf8.DecodeRune: an invalid sequence is U+FFFD of one byte. */
function decodeGoRune(bytes: Buffer, index: number): [number, number] {
  const first = bytes[index]!;
  if (first < 0x80) return [first, 1];
  const continuation = (offset: number, low = 0x80, high = 0xbf) => { const byte = bytes[index + offset]; return byte !== undefined && byte >= low && byte <= high ? byte & 0x3f : -1; };
  if (first >= 0xc2 && first <= 0xdf) {
    const second = continuation(1);
    return second < 0 ? [0xfffd, 1] : [((first & 0x1f) << 6) | second, 2];
  }
  if (first >= 0xe0 && first <= 0xef) {
    const second = continuation(1, first === 0xe0 ? 0xa0 : 0x80, first === 0xed ? 0x9f : 0xbf);
    const third = second < 0 ? -1 : continuation(2);
    return third < 0 ? [0xfffd, 1] : [((first & 0x0f) << 12) | (second << 6) | third, 3];
  }
  if (first >= 0xf0 && first <= 0xf4) {
    const second = continuation(1, first === 0xf0 ? 0x90 : 0x80, first === 0xf4 ? 0x8f : 0xbf);
    const third = second < 0 ? -1 : continuation(2);
    const fourth = third < 0 ? -1 : continuation(3);
    return fourth < 0 ? [0xfffd, 1] : [((first & 0x07) << 18) | (second << 12) | (third << 6) | fourth, 4];
  }
  return [0xfffd, 1];
}
