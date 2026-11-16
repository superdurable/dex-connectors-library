// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';

/** Version of the stored rowAdded cursor token. */
export const RowAddedCursorVersion = 1;

/** The stored poll position: the last processed row number and a SHA-256 of its cells. */
export interface RowAddedCursorToken {
  /** The last processed 1-based row number; row 1 is the header. */ lastRow: number;
  /** Hex SHA-256 of the row's cells as Go encodes them, without trailing empty cells. */ rowHash: string;
}

const cursorFieldNames: Readonly<Record<string, 'v' | 'lastRow' | 'rowHash'>> = { v: 'v', lastrow: 'lastRow', rowhash: 'rowHash' };
const integerSource = /^-?(?:0|[1-9][0-9]*)$/;
const hexDigits = /^[0-9a-fA-F]{64}$/;

/** Return Go's json.Marshal text of the token, `{"v":1,"lastRow":N,"rowHash":"<hex>"}`. */
export function encodeRowAddedCursorToken(token: RowAddedCursorToken): string {
  return `{"v":${RowAddedCursorVersion},"lastRow":${token.lastRow},"rowHash":${encodeGoJSONValue(token.rowHash)}}`;
}

/**
 * Decode a stored token as Go's json.Decoder with DisallowUnknownFields does: field names match
 * case-insensitively, a later duplicate wins, null leaves a field zero, and integers have no fraction or
 * exponent. Throws for a token Go refuses or one of another shape.
 */
export function decodeRowAddedCursorToken(encoded: string): RowAddedCursorToken {
  let document: unknown;
  try {
    document = JSON.parse(encoded, (_key, value: unknown, context?: { source?: string }) => {
      if (typeof value === 'number' && (context?.source === undefined || !integerSource.test(context.source) || !Number.isSafeInteger(value))) throw new TypeError('cursor integers have no fraction or exponent');
      return value;
    });
  } catch { throw new TypeError('Google Sheets rowAdded cursor is not valid JSON'); }
  if (document !== null && (typeof document !== 'object' || Array.isArray(document))) throw new TypeError('Google Sheets rowAdded cursor is not valid JSON');
  const fields: { v: unknown; lastRow: unknown; rowHash: unknown } = { v: 0, lastRow: 0, rowHash: '' };
  for (const [key, value] of Object.entries(document ?? {})) {
    const name = cursorFieldNames[key.toLowerCase()];
    if (name === undefined) throw new TypeError('Google Sheets rowAdded cursor is not valid JSON');
    if (value === null) continue;
    if (name === 'rowHash' ? typeof value !== 'string' : typeof value !== 'number') throw new TypeError('Google Sheets rowAdded cursor is not valid JSON');
    fields[name] = value;
  }
  const { v: version, lastRow, rowHash } = fields as { v: number; lastRow: number; rowHash: string };
  if (version !== RowAddedCursorVersion || lastRow < 1 || rowHash.length !== 64) throw new TypeError('Google Sheets rowAdded cursor has an unsupported shape');
  if (!hexDigits.test(rowHash)) throw new TypeError('Google Sheets rowAdded cursor hash is not hexadecimal');
  return { lastRow, rowHash };
}

/** Return cells without trailing empty strings, as Sheets omits them. */
export function withoutTrailingEmptyCells(cells: readonly string[]): string[] {
  let end = cells.length;
  while (end > 0 && cells[end - 1] === '') end--;
  return cells.slice(0, end);
}

/** Return the hex SHA-256 of Go's JSON encoding of cells without trailing empty cells. */
export function hashRowCells(cells: readonly string[] | undefined): string {
  return createHash('sha256').update(encodeGoJSONValue(withoutTrailingEmptyCells(cells ?? [])), 'utf8').digest('hex');
}

/** Return `row-<row number>-<16 hex>`, a pure function of the tab, row number, and cells. */
export function rowAddedEventId(spreadsheetId: string, sheetName: string, rowNumber: number, cells: readonly string[]): string {
  // Buffer encodes an unpaired surrogate as U+FFFD, as Go holds a JSON-decoded string.
  const identity = [spreadsheetId, sheetName, String(rowNumber), encodeGoJSONValue([...cells])].join('\u0000');
  return `row-${rowNumber}-${createHash('sha256').update(Buffer.from(identity, 'utf8')).digest('hex').slice(0, 16)}`;
}

/** Map each non-empty, unique header to its cell, or "" when the row is shorter. */
export function headerKeyedValues(header: readonly string[], cells: readonly string[]): Record<string, string> {
  const counts = new Map<string, number>();
  for (const name of header) counts.set(name, (counts.get(name) ?? 0) + 1);
  const values: Record<string, string> = {};
  header.forEach((name, column) => {
    if (name && counts.get(name) === 1) values[name] = cells[column] ?? '';
  });
  return values;
}
