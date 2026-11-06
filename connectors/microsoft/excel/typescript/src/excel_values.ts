// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { AppendTableRowsInput, GetTableRowsInput, GetValuesInput, UpdateValuesInput } from './generated/models.js';
import { graphJSON } from './graph.js';

/** Parsed A1 range after Excel bounds checks. */
export interface A1Range { address: string; rowCount: number; columnCount: number; }
/** One table column in its defined order. */
export interface TableColumn { id: string; index: number; name: string; }
/** Graph range data after shape validation. */
export interface RangeData { address: string; rowCount: number; columnCount: number; values: Array<Array<string | number | boolean | null>>; text: string[][]; }

const graphID = /^[A-Za-z0-9!_.~-]{1,512}$/;
const worksheetID = /^\{[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\}$/i;
const a1Cell = /^([A-Z]{1,3})([1-9][0-9]{0,6})$/;

/** Validate Graph Drive and item identifiers. */
export function validateWorkbook(driveId: string, workbookId: string): void {
  if (!graphID.test(driveId)) throw new TypeError('driveId must be a Microsoft Graph drive ID, such as the one the workbookPicker unit stores');
  if (!graphID.test(workbookId)) throw new TypeError('workbookId must be a Microsoft Graph drive item ID, such as the one the workbookPicker unit stores');
}
/** Validate a worksheet ID or Excel name. */
export function validateWorksheet(worksheet: string): void {
  if (worksheetID.test(worksheet)) return;
  const characters = [...worksheet];
  if (characters.length < 1 || characters.length > 31) throw new TypeError('worksheet must be a worksheet ID or a name of 1 to 31 characters');
  if ([...'\\/?*[]:'].some(character => worksheet.includes(character)) || /\p{Cc}/u.test(worksheet))
    throw new TypeError('worksheet name cannot contain \\ / ? * [ ] : or control characters');
  if (worksheet.startsWith("'") || worksheet.endsWith("'")) throw new TypeError('worksheet name cannot begin or end with an apostrophe');
}
/** Validate a table or column name. */
export function validateTableText(value: string, field: 'table' | 'column'): void {
  if (![...value].length || [...value].length > 255 || !trimGoSpace(value) || value.includes('/') || /\p{Cc}/u.test(value))
    throw new TypeError(field === 'table' ? 'table must be a table ID or name of 1 to 255 characters without slashes or control characters' : 'a column name must be 1 to 255 characters without slashes or control characters');
}
/** Validate one workbook table location. */
export function validateTableLocation(input: GetTableRowsInput | AppendTableRowsInput): void {
  validateWorkbook(input.driveId, input.workbookId); validateTableText(input.table, 'table');
}
function trimGoSpace(value: string): string {
  const isSpace = (character: string): boolean => {
    const code = character.codePointAt(0)!;
    return code >= 9 && code <= 13 || code === 32 || code === 133 || code === 160 || code === 5760 ||
      code >= 8192 && code <= 8202 || code === 8232 || code === 8233 || code === 8239 || code === 8287 || code === 12288;
  };
  let start = 0, end = value.length;
  while (start < end && isSpace(value[start]!)) start++;
  while (end > start && isSpace(value[end - 1]!)) end--;
  return value.slice(start, end);
}
/** Parse a bounded A1 cell or rectangle. */
export function parseA1Range(value: string): A1Range {
  const address = trimGoSpace(value).replace(/[a-z\u0131\u017f]/g, character => character.toUpperCase());
  const parts = address.split(':');
  if (parts.length > 2) throw new TypeError('address must be one cell or range in A1 form, such as A1 or A2:C10, without a worksheet name, $, or whole columns or rows');
  const first = parseCell(parts[0] ?? ''); const last = parseCell(parts[1] ?? parts[0] ?? '');
  if (last.column < first.column || last.row < first.row) throw new TypeError('address must name its top-left cell first, such as A1:C3');
  return { address, rowCount: last.row - first.row + 1, columnCount: last.column - first.column + 1 };
}
/** Validate one bounded worksheet range location. */
export function validateRange(input: GetValuesInput | UpdateValuesInput, maxCells: number): A1Range {
  validateWorkbook(input.driveId, input.workbookId); validateWorksheet(input.worksheet);
  const range = parseA1Range(input.address);
  if (range.rowCount * range.columnCount > maxCells) throw new TypeError('address covers more cells than the configured cell limit');
  return range;
}
function parseCell(value: string): { column: number; row: number } {
  const match = a1Cell.exec(value);
  if (!match) throw new TypeError('address must be one cell or range in A1 form, such as A1 or A2:C10, without a worksheet name, $, or whole columns or rows');
  let column = 0; for (const character of match[1]!) column = column * 26 + character.charCodeAt(0) - 64;
  const row = Number(match[2]);
  if (column > 16384 || row > 1048576) throw new TypeError("address is outside Excel's worksheet limits of column XFD and row 1048576");
  return { column, row };
}

/** Decode Graph table columns with unique names and contiguous indexes. */
export function decodeColumns(body: Uint8Array): TableColumn[] {
  const decoded = graphJSON(body) as { value?: unknown };
  if (!decoded || !Array.isArray(decoded.value) || decoded.value.length === 0) throw new TypeError('table columns response is invalid');
  const columns: TableColumn[] = [];
  const names = new Set<string>();
  for (const value of decoded.value) {
    const column = value as Partial<TableColumn>;
    if (!column || typeof column.id !== 'string' || !column.id || typeof column.name !== 'string' || !column.name ||
      !Number.isSafeInteger(column.index) || names.has(column.name)) throw new TypeError('table column is invalid');
    names.add(column.name); columns.push({ id: column.id, name: column.name, index: column.index! });
  }
  columns.sort((left, right) => left.index - right.index);
  if (columns.some((column, index) => column.index !== index)) throw new TypeError('table column positions are not contiguous');
  return columns;
}

/** Decode Graph range cells without coercing invalid cell types. */
export function decodeRange(body: Uint8Array, requireText: boolean): RangeData {
  const decoded = graphJSON(body) as { address?: unknown; rowCount?: unknown; columnCount?: unknown; values?: unknown; text?: unknown };
  if (!decoded || !Number.isSafeInteger(decoded.rowCount) || !Number.isSafeInteger(decoded.columnCount) ||
    (decoded.rowCount as number) < 0 || (decoded.columnCount as number) < 0) throw new TypeError('range response is invalid');
  const rowCount = decoded.rowCount as number; const columnCount = decoded.columnCount as number;
  if (decoded.values === undefined || decoded.values === null) throw new MissingValuesError();
  if (!validGrid(decoded.values, rowCount, columnCount, validCell)) throw new TypeError('range values do not match the range size');
  if (requireText && !validGrid(decoded.text, rowCount, columnCount, value => typeof value === 'string')) throw new TypeError('range text does not match the range size');
  return { address: typeof decoded.address === 'string' ? decoded.address : '', rowCount, columnCount,
    values: decoded.values as RangeData['values'], text: requireText ? decoded.text as string[][] : [] };
}
/** Missing Graph values means Excel omitted an oversized range. */
export class MissingValuesError extends Error {}
function validGrid(value: unknown, rows: number, columns: number, validate: (value: unknown) => boolean): boolean {
  return Array.isArray(value) && value.length === rows && value.every(row => Array.isArray(row) && row.length === columns && row.every(validate));
}
function validCell(value: unknown): boolean { return value === null || typeof value === 'string' || typeof value === 'boolean' || typeof value === 'number' && Number.isFinite(value); }

/** Encode one literal Excel cell, preventing formulas and number coercion. */
export function wireCell(value: unknown): string | number | boolean {
  if (typeof value === 'string') return value ? `'${value}` : '';
  if (typeof value === 'boolean') return value;
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  throw new TypeError(value === null || value === undefined ? 'a value is absent; write EmptyCellValue to clear a cell' : 'a number value is NaN or infinite');
}
/** Validate a rectangular absolute write. */
export function validateUpdate(input: UpdateValuesInput, maxCells: number): { range: A1Range; values: Array<Array<string | number | boolean>> } {
  const range = validateRange(input, maxCells);
  if (!Array.isArray(input.values) || input.values.length !== range.rowCount) throw new TypeError(`values has ${Array.isArray(input.values) ? input.values.length : 0} rows but the address has ${range.rowCount}`);
  const values = input.values.map((row, rowIndex) => {
    if (!Array.isArray(row) || row.length !== range.columnCount) throw new TypeError(`values row ${rowIndex + 1} has ${Array.isArray(row) ? row.length : 0} cells but the address has ${range.columnCount} columns`);
    return row.map((cell, columnIndex) => {
      try { return wireCell(cell); } catch (error) { throw new TypeError(`values row ${rowIndex + 1} cell ${columnIndex + 1}: ${error instanceof Error ? error.message : 'invalid cell'}`); }
    });
  });
  return { range, values };
}
