// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { COQLValue, FindRecordsInput, ListModifiedRecordsInput, RecordCondition, RecordSort } from './models.js';

const namePattern = /^[A-Za-z][A-Za-z0-9_]{0,99}$/;
const pathPattern = /^[A-Za-z][A-Za-z0-9_]{0,99}(?:\.[A-Za-z][A-Za-z0-9_]{0,99}){0,2}$/;
const idPattern = /^[0-9]{1,20}$/;

/** Require a Zoho CRM module API name. */
export function validateModule(module: string): void {
  if (!namePattern.test(module)) throw new TypeError('module must be a Zoho CRM module API name such as Contacts, Accounts, or Deals');
}

/** Require a numeric Zoho CRM record identifier. */
export function validateRecordId(recordId: string): void {
  if (!idPattern.test(recordId)) throw new TypeError('recordId must be a numeric Zoho CRM record ID such as 4150868000000376008');
}

/** Check selected field paths and duplicates. */
export function validateFields(fields: string[], minimum: number): void {
  if (fields.length < minimum || fields.length > 50) throw new TypeError(`fields must name ${minimum} to 50 field API names`);
  const seen = new Set<string>();
  for (const field of fields) {
    if (!pathPattern.test(field) || seen.has(field)) throw new TypeError(`field "${field}" must be a distinct Zoho CRM field API name such as Last_Name or Account_Name.Account_Name`);
    seen.add(field);
  }
}

function quoteText(value: string): string {
  if (!value || Array.from(value).length > 1000 || Array.from(value).some(char => {
    const codePoint = char.codePointAt(0)!;
    return codePoint >= 0xD800 && codePoint <= 0xDFFF;
  })) throw new TypeError('a text value must be 1 to 1000 characters of valid UTF-8');
  if (value.includes("'") || value.includes('\\') || Array.from(value).some(char => char.codePointAt(0)! < 32 || (char.codePointAt(0)! >= 127 && char.codePointAt(0)! <= 159))) throw new TypeError('a text value cannot contain an apostrophe, a backslash, or a control character, which COQL cannot quote safely');
  return `'${value}'`;
}

function validDate(text: string, withTime: boolean): boolean {
  const pattern = withTime ? /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})([+-])(\d{2}):(\d{2})$/ : /^(\d{4})-(\d{2})-(\d{2})$/;
  const match = pattern.exec(text);
  if (!match) return false;
  const year = Number(match[1]), month = Number(match[2]), day = Number(match[3]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const monthDays = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > monthDays[month - 1]!) return false;
  if (!withTime) return true;
  return Number(match[4]) < 24 && Number(match[5]) < 60 && Number(match[6]) < 60 && Number(match[8]) <= 24 && Number(match[9]) < 60;
}

function renderValue(value: COQLValue): string {
  if (value.kind === 'text') return quoteText(value.text ?? '');
  if (value.kind === 'textList' || value.kind === 'recordIdList') {
    const values = value.texts ?? [];
    if (values.length < 1 || values.length > 100) throw new TypeError('a list value must hold 1 to 100 entries');
    if (value.kind === 'recordIdList' && values.some(item => !idPattern.test(item))) throw new TypeError('a record ID list may hold only numeric IDs');
    return `(${values.map(quoteText).join(', ')})`;
  }
  if (value.kind === 'number') {
    if (!/^-?[0-9]{1,18}(?:\.[0-9]{1,9})?$/.test(value.text ?? '')) throw new TypeError('a number value must be a plain decimal such as 50000 or 1250.50');
    return value.text!;
  }
  if (value.kind === 'boolean') return value.boolean ? 'true' : 'false';
  if (value.kind === 'date') {
    if (!validDate(value.text ?? '', false)) throw new TypeError('a date value must be YYYY-MM-DD');
    return `'${value.text}'`;
  }
  if (value.kind === 'dateTime') {
    if (!validDate(value.text ?? '', true)) throw new TypeError('a datetime value must be built with COQLDateTime');
    return `'${value.text}'`;
  }
  if (value.kind === 'recordId') {
    if (!idPattern.test(value.text ?? '')) throw new TypeError('a record ID value must be numeric');
    return `'${value.text}'`;
  }
  throw new TypeError('value kind is not supported');
}

function renderCondition(condition: RecordCondition): string {
  if (!pathPattern.test(condition.field)) throw new TypeError('field must be a Zoho CRM field API name such as Email or Account_Name.Account_Name');
  const operator = condition.operator, value = condition.value;
  if (operator === 'is null' || operator === 'is not null') {
    if (value !== undefined) throw new TypeError(`${operator} takes no value`);
    return `${condition.field} ${operator}`;
  }
  if (operator === 'in' || operator === 'not in') {
    if (!value || !['textList', 'recordIdList'].includes(value.kind)) throw new TypeError(`${operator} needs a COQLTextList or COQLRecordIDList value`);
  } else if (operator === '=' || operator === '!=') {
    if (!value || ['textList', 'recordIdList'].includes(value.kind)) throw new TypeError(`${operator} needs one value, not a list`);
  } else if (['>', '>=', '<', '<='].includes(operator)) {
    if (!value || !['number', 'date', 'dateTime'].includes(value.kind)) throw new TypeError(`${operator} needs a number, date, or datetime value`);
  } else throw new TypeError('operator must be one of = != > >= < <= in, not in, is null, or is not null');
  return `${condition.field} ${operator} ${renderValue(value)}`;
}

function renderConditions(conditions: RecordCondition[]): string {
  if (conditions.length < 1 || conditions.length > 10) throw new TypeError('conditions must hold 1 to 10 conditions');
  let rendered = '';
  conditions.forEach((condition, index) => {
    let part: string;
    try { part = renderCondition(condition); }
    catch (error) { throw new TypeError(`condition ${index + 1}: ${String(error)}`); }
    rendered = index === 0 ? part : `(${rendered} and ${part})`;
  });
  return rendered;
}

function renderSort(sort?: RecordSort): string {
  if (!sort || sort.field === 'id') return sort?.isDescending ? 'id desc' : 'id asc';
  if (!pathPattern.test(sort.field)) throw new TypeError('sort field must be a Zoho CRM field API name such as Modified_Time');
  return `${sort.field} ${sort.isDescending ? 'desc' : 'asc'}, id asc`;
}

function selectQuery(module: string, fields: string[], condition: string, sort: string, offset: number, limit: number): string {
  return `select ${fields.filter(field => field !== 'id').join(', ') || 'id'} from ${module} where ${condition} order by ${sort} limit ${offset}, ${limit}`;
}

/** Build a safe COQL query matching the Go connector. */
export function buildFindRecordsQuery(input: FindRecordsInput): string {
  validateModule(input.module); validateFields(input.fields ?? [], 1);
  const limit = input.limit || 10, offset = input.offset ?? 0;
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > 200) throw new TypeError('limit must be 1 to 200');
  if (!Number.isSafeInteger(offset) || offset < 0 || offset + limit > 100_000) throw new TypeError("offset plus limit must not pass Zoho CRM's 100000-record COQL depth");
  return selectQuery(input.module, input.fields!, renderConditions(input.conditions ?? []), renderSort(input.sort), offset, limit);
}

/** Parse the immutable watermark and render the next modified record query. */
export function buildListModifiedRecordsQuery(input: ListModifiedRecordsInput): { query: string; cursor: string } {
  validateModule(input.module);
  if (!(input.fields?.length)) throw new TypeError('fields must name 1 to 49 field API names');
  const fields = input.fields.includes('Modified_Time') ? input.fields : [...input.fields, 'Modified_Time'];
  validateFields(fields, 1);
  const limit = input.limit || 100;
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > 200) throw new TypeError('limit must be 1 to 200');
  if (input.cursor && input.modifiedSince) throw new TypeError('set modifiedSince to start a feed or cursor to continue one, not both');
  if (!input.cursor && !input.modifiedSince) throw new TypeError('modifiedSince or cursor is required');
  let at: string, id: string;
  if (input.cursor) {
    const match = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z)\/([0-9]{1,20})$/.exec(input.cursor);
    if (!match || Number.isNaN(Date.parse(match[1]!)) || new Date(match[1]!).toISOString().replace(/\.\d{3}Z$/, 'Z') !== match[1]) throw new TypeError('cursor must be a cursor returned by listModifiedRecords');
    at = match[1]!; id = match[2]!.replace(/^0+/, '') || '0';
  } else {
    const date = new Date(input.modifiedSince!);
    if (Number.isNaN(date.getTime())) throw new TypeError('modifiedSince or cursor is required');
    at = date.toISOString().replace(/\.\d{3}Z$/, 'Z'); id = '0';
  }
  const literal = `'${at.replace('Z', '+00:00')}'`;
  const condition = id === '0' ? `Modified_Time >= ${literal}` : `(Modified_Time > ${literal} or (Modified_Time = ${literal} and id > ${id}))`;
  return { query: selectQuery(input.module, fields, condition, 'Modified_Time asc, id asc', 0, limit), cursor: `${at}/${id}` };
}

/** Return whether a string is a valid field API name. */
export const isFieldName = (value: string): boolean => namePattern.test(value);
/** Return whether a string is a valid field path. */
export const isFieldPath = (value: string): boolean => pathPattern.test(value);
/** Return whether a string is a valid record ID. */
export const isRecordId = (value: string): boolean => idPattern.test(value);
