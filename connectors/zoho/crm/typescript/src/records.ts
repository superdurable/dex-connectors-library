// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ModuleField, Record as ZohoRecord } from './models.js';
import { isFieldName, isFieldPath, isRecordId } from './coql.js';

function object(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
}

/** Decode JSON and preserve field numbers as raw JSON tokens. */
export function parseExactJSON(body: Uint8Array): unknown {
  const text = new TextDecoder('utf-8', { fatal: true }).decode(body);
  const exactJSON = JSON as typeof JSON & { rawJSON(source: string): unknown; isRawJSON(value: unknown): boolean; parse(source: string, reviver: (key: string, value: unknown, context: { source?: string }) => unknown): unknown };
  return exactJSON.parse(text, (_key: string, value: unknown, context: { source?: string }) => {
    if (typeof value === 'number') {
      if (!context.source) throw new TypeError('Zoho CRM returned a number without exact source text');
      return exactJSON.rawJSON(context.source);
    }
    return value;
  }) as unknown;
}

/** Decode a Go int, rejecting exponent, decimal, NaN, Infinity and unsafe values. */
export function strictInteger(value: unknown, field: string): number {
  const exactJSON = JSON as typeof JSON & { isRawJSON(value: unknown): boolean };
  const source = typeof value === 'number' ? String(value) : exactJSON.isRawJSON(value) ? (value as { rawJSON: string }).rawJSON : '';
  if (!/^-?(?:0|[1-9][0-9]*)$/.test(source)) throw new TypeError(`${field} must be an integer`);
  const number = Number(source);
  if (!Number.isSafeInteger(number)) throw new TypeError(`${field} exceeds exact integer precision`);
  return number;
}

/** Decode a Zoho page while keeping arbitrary field values exact. */
export function decodeRecordPage(module: string, body: Uint8Array): { records: ZohoRecord[]; hasMore: boolean } {
  if (!new TextDecoder().decode(body).trim()) return { records: [], hasMore: false };
  let page: Record<string, unknown> | undefined;
  try { page = object(parseExactJSON(body)); }
  catch { throw new TypeError('the page is not a JSON object with a data list'); }
  if (!page) throw new TypeError('the page is not a JSON object with a data list');
  const data = page.data;
  if (data !== undefined && data !== null && !Array.isArray(data)) throw new TypeError('the page is not a JSON object with a data list');
  const records: ZohoRecord[] = [];
  for (const entry of data ?? []) {
    const fields = object(entry);
    if (!fields) throw new TypeError('a record is not a JSON object');
    if (typeof fields.id !== 'string' || !isRecordId(fields.id)) throw new TypeError('a record has no numeric id');
    const selected: Record<string, unknown> = {};
    for (const [name, value] of Object.entries(fields)) if (name !== 'id' && isFieldPath(name)) selected[name] = value;
    records.push({ module, id: fields.id, fields: selected });
  }
  const info = object(page.info);
  if (info?.more_records !== undefined && info.more_records !== null && typeof info.more_records !== 'boolean') throw new TypeError('the page is not a JSON object with a data list');
  return { records, hasMore: info?.more_records === true };
}

/** Parse a provider RFC3339 timestamp without losing nanosecond precision. */
export function parseZohoTime(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined;
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))$/.exec(value);
  if (!match) return undefined;
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number);
  const leap = year! % 4 === 0 && (year! % 100 !== 0 || year! % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month! < 1 || month! > 12 || day! < 1 || day! > days[month! - 1]! || hour! > 23 || minute! > 59 || second! > 59) return undefined;
  if (match[8] !== 'Z' && (Number(match[10]) > 24 || Number(match[11]) > 60)) return undefined;
  const fraction = (match[7] ?? '').slice(0, 9).replace(/0+$/, '');
  const offset = match[8] === 'Z' || (match[10] === '00' && match[11] === '00') ? 'Z' : match[8];
  return `${match[1]}-${match[2]}-${match[3]}T${match[4]}:${match[5]}:${match[6]}${fraction ? '.' + fraction : ''}${offset}`;
}

/** Decode Zoho's one-result write response or return its record-level error. */
export function decodeWriteResult(body: Uint8Array): { id: string; action: string; duplicateField: string; createdAt?: string; modifiedAt?: string; error?: Record<string, unknown> } {
  let root: Record<string, unknown> | undefined;
  try { root = object(parseExactJSON(body)); }
  catch { throw new TypeError('the response is not a JSON object with a data list'); }
  if (!root || !Array.isArray(root.data)) throw new TypeError('the response is not a JSON object with a data list');
  if (root.data.length !== 1) throw new TypeError(`the response holds ${root.data.length} results for one record`);
  const entry = object(root.data[0]);
  if (entry?.status === 'error') return { id: '', action: '', duplicateField: '', error: entry };
  if (!entry || entry.code !== 'SUCCESS') throw new TypeError('the result is not a SUCCESS entry');
  const details = object(entry.details);
  if (!details || typeof details.id !== 'string' || !isRecordId(details.id)) throw new TypeError('the result has no numeric record id');
  const createdAt = parseZohoTime(details.Created_Time);
  const modifiedAt = parseZohoTime(details.Modified_Time);
  return { id: details.id, action: typeof entry.action === 'string' ? entry.action : '',
    duplicateField: typeof entry.duplicate_field === 'string' && isFieldName(entry.duplicate_field) ? entry.duplicate_field : '',
    ...(createdAt ? { createdAt } : {}),
    ...(modifiedAt ? { modifiedAt } : {}) };
}

/** Decode module fields with strict Go integer semantics. */
export function decodeModuleFields(body: Uint8Array): ModuleField[] {
  let root: Record<string, unknown> | undefined;
  try { root = object(parseExactJSON(body)); }
  catch { throw new TypeError('the response is not a JSON object with a fields list'); }
  if (!root || !Array.isArray(root.fields)) throw new TypeError('the response is not a JSON object with a fields list');
  return root.fields.map(entry => {
    const wire = object(entry);
    if (!wire || typeof wire.api_name !== 'string' || !isFieldName(wire.api_name) || typeof wire.data_type !== 'string' || !wire.data_type) throw new TypeError('a field has no valid api_name or data_type');
    for (const name of ['display_label', 'data_type', 'api_name']) {
      if (wire[name] !== undefined && wire[name] !== null && typeof wire[name] !== 'string') throw new TypeError(`field ${name} must be a string`);
    }
    for (const name of ['custom_field', 'read_only', 'system_mandatory']) {
      if (wire[name] !== undefined && wire[name] !== null && typeof wire[name] !== 'boolean') throw new TypeError(`field ${name} must be a boolean`);
    }
    if (wire.lookup !== undefined && wire.lookup !== null && !object(wire.lookup)) throw new TypeError('field lookup must be an object');
    const lookupWire = object(wire.lookup);
    if (lookupWire?.module !== undefined && lookupWire.module !== null && !object(lookupWire.module)) throw new TypeError('field lookup.module must be an object');
    const lookup = object(object(wire.lookup)?.module);
    if (lookup?.api_name !== undefined && lookup.api_name !== null && typeof lookup.api_name !== 'string') throw new TypeError('field lookup.module.api_name must be a string');
    if (wire.pick_list_values !== undefined && wire.pick_list_values !== null && !Array.isArray(wire.pick_list_values)) throw new TypeError('field pick_list_values must be an array');
    const picklist = Array.isArray(wire.pick_list_values) ? wire.pick_list_values.map(value => {
      const item = value === null ? {} : object(value);
      if (!item) throw new TypeError('picklist entry must be an object');
      for (const name of ['actual_value', 'display_value', 'type']) {
        if (item[name] !== undefined && item[name] !== null && typeof item[name] !== 'string') throw new TypeError(`picklist ${name} must be a string`);
      }
      return { actualValue: typeof item.actual_value === 'string' ? item.actual_value : '', ...(typeof item.display_value === 'string' && item.display_value ? { displayValue: item.display_value } : {}), ...(item.type === 'unused' ? { isUnused: true } : {}) };
    }) : [];
    const length = wire.length === undefined || wire.length === null ? 0 : strictInteger(wire.length, 'field length');
    const unique = object(wire.unique);
    return { apiName: wire.api_name, dataType: wire.data_type,
      ...(typeof wire.display_label === 'string' && wire.display_label ? { displayLabel: wire.display_label } : {}),
      ...(wire.custom_field === true ? { isCustom: true } : {}), ...(wire.read_only === true ? { isReadOnly: true } : {}),
      ...(wire.system_mandatory === true ? { isSystemMandatory: true } : {}), ...(unique && Object.keys(unique).length ? { isUnique: true } : {}),
      ...(length > 0 ? { length } : {}), ...(typeof lookup?.api_name === 'string' && isFieldName(lookup.api_name) ? { lookupModule: lookup.api_name } : {}),
      ...(picklist.length ? { picklistValues: picklist } : {}) };
  });
}

/** Validate and return write fields without coalescing exact numeric values. */
export function validateWriteFields(fields: Record<string, unknown> | null): Record<string, unknown> {
  if (!fields || Object.keys(fields).length < 1 || Object.keys(fields).length > 200) throw new TypeError('fields must set 1 to 200 fields');
  for (const [name, value] of Object.entries(fields)) {
    if (!isFieldName(name) || name.toLowerCase() === 'id') throw new TypeError(`field name "${name}" must be a Zoho CRM field API name other than id`);
    if (value === undefined || (typeof value === 'number' && !Number.isFinite(value))) throw new TypeError(`field ${name} must hold one JSON value`);
    if (typeof value === 'number' && !Number.isSafeInteger(value)) throw new TypeError(`field ${name} exceeds exact numeric precision`);
    try { JSON.stringify(value); } catch { throw new TypeError(`field ${name} must hold one JSON value`); }
  }
  return fields;
}
