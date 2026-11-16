// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';

/** Thrown for a ticketChanged cursor token that is not a valid Go cursor document. */
export class TicketExportCursorInvalidError extends Error {
  /** Describe the rejected token without echoing it. */
  constructor() { super('Zendesk incremental export cursor is not decodable'); this.name = 'TicketExportCursorInvalidError'; }
}

/**
 * The Go connector's committed cursor. resumeAt is a Unix time at or before every change after afterCursor, so a
 * rejected or missing cursor restarts the export from it without a gap.
 */
export interface TicketExportCursor { afterCursor: string; resumeAt: bigint; includeDeleted: boolean }

const int64Minimum = -(1n << 63n);
const int64Maximum = (1n << 63n) - 1n;

/** Encode the cursor as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings. */
export function encodeTicketExportCursor(position: TicketExportCursor): Cursor {
  const members: string[] = [];
  if (position.afterCursor) members.push(`"afterCursor":${encodeGoJSONValue(position.afterCursor)}`);
  members.push(`"resumeAt":${position.resumeAt.toString()}`);
  if (position.includeDeleted) members.push('"includeDeleted":true');
  return { token: `{${members.join(',')}}` };
}

/**
 * Decode the token as Go's encoding/json fills the cursor struct: keys match case-insensitively, the last
 * duplicate wins, unknown keys and null values are ignored, and a value of the wrong JSON type rejects the token,
 * as does a resumeAt below 1.
 */
export function decodeTicketExportCursor(token: string): TicketExportCursor {
  try { JSON.parse(token); } catch { throw new TicketExportCursorInvalidError(); }
  const position: TicketExportCursor = { afterCursor: '', resumeAt: 0n, includeDeleted: false };
  const text = token.trim();
  if (text !== 'null') {
    if (!text.startsWith('{')) throw new TicketExportCursorInvalidError();
    for (const [name, raw] of topLevelMembers(text)) {
      const folded = goFoldName(name);
      if (raw === 'null') continue;
      if (folded === 'aftercursor') {
        if (!raw.startsWith('"')) throw new TicketExportCursorInvalidError();
        position.afterCursor = JSON.parse(raw) as string;
      } else if (folded === 'resumeat') {
        if (!/^-?(?:0|[1-9]\d*)$/.test(raw)) throw new TicketExportCursorInvalidError();
        const value = BigInt(raw);
        if (value < int64Minimum || value > int64Maximum) throw new TicketExportCursorInvalidError();
        position.resumeAt = value;
      } else if (folded === 'includedeleted') {
        if (raw !== 'true' && raw !== 'false') throw new TicketExportCursorInvalidError();
        position.includeDeleted = raw === 'true';
      }
    }
  }
  if (position.resumeAt < 1n) throw new TicketExportCursorInvalidError();
  return position;
}

// Go matches JSON keys to fields with Unicode simple folding, under which ſ is s and the Kelvin sign is k.
function goFoldName(name: string): string {
  return name.replace(/ſ/g, 's').replace(/K/g, 'k').toLowerCase();
}

/** Split a valid JSON object into its keys and raw value texts, in document order. */
function topLevelMembers(text: string): Array<[string, string]> {
  const members: Array<[string, string]> = [];
  let index = skipWhitespace(text, 1);
  if (text[index] === '}') return members;
  for (;;) {
    const keyEnd = skipString(text, index);
    const key = JSON.parse(text.slice(index, keyEnd)) as string;
    index = skipWhitespace(text, skipWhitespace(text, keyEnd) + 1);
    const valueEnd = skipValue(text, index);
    members.push([key, text.slice(index, valueEnd)]);
    index = skipWhitespace(text, valueEnd);
    if (text[index] === '}') return members;
    index = skipWhitespace(text, index + 1);
  }
}

function skipWhitespace(text: string, index: number): number {
  while (index < text.length && ' \t\n\r'.includes(text[index]!)) index++;
  return index;
}

function skipString(text: string, index: number): number {
  for (let cursor = index + 1; cursor < text.length; cursor++) {
    if (text[cursor] === '\\') cursor++;
    else if (text[cursor] === '"') return cursor + 1;
  }
  throw new TicketExportCursorInvalidError();
}

function skipValue(text: string, index: number): number {
  if (text[index] === '"') return skipString(text, index);
  if (text[index] !== '{' && text[index] !== '[') {
    let end = index;
    while (end < text.length && !',}] \t\n\r'.includes(text[end]!)) end++;
    return end;
  }
  let depth = 0;
  for (let cursor = index; cursor < text.length; cursor++) {
    const character = text[cursor]!;
    if (character === '"') cursor = skipString(text, cursor) - 1;
    else if (character === '{' || character === '[') depth++;
    else if (character === '}' || character === ']') {
      depth--;
      if (depth === 0) return cursor + 1;
    }
  }
  throw new TicketExportCursorInvalidError();
}
