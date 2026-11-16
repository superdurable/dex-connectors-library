// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { isRecordID } from './client.js';
import type { Record as SalesforceRecord } from './generated/models.js';
import { RecordChangeCreated, RecordChangeUpdated, type RecordChangeKind } from './record_modified.js';

/** A UTC time with Go's nanosecond precision: Unix milliseconds and the nanoseconds below them. */
export interface Instant { milliseconds: number; nanoseconds: number }

const salesforceDateTime = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2}|[+-]\d{4})$/;
const rfc3339 = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/;
const cursorFieldNames = ['sObjectType', 'systemModstamp', 'recordId'] as const;

function parseInstant(match: RegExpExecArray | null): Instant | undefined {
  if (!match) return undefined;
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number) as [number, number, number, number, number, number];
  const fraction = match[7] ?? '';
  const zone = match[8]!;
  const local = new Date(0);
  local.setUTCFullYear(year, month - 1, day);
  local.setUTCHours(hour, minute, second, 0);
  if (month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59 || local.getUTCFullYear() !== year ||
    local.getUTCMonth() + 1 !== month || local.getUTCDate() !== day) return undefined;
  let offsetMinutes = 0;
  if (zone !== 'Z') {
    const digits = zone.slice(1).replace(':', '');
    const hours = Number(digits.slice(0, 2));
    const minutes = Number(digits.slice(2));
    if (hours >= 24 || minutes >= 60) return undefined;
    offsetMinutes = (hours * 60 + minutes) * (zone.startsWith('-') ? -1 : 1);
  }
  const nanoseconds = Number(fraction.slice(0, 9).padEnd(9, '0'));
  return { milliseconds: local.getTime() - offsetMinutes * 60_000 + Math.floor(nanoseconds / 1_000_000), nanoseconds: nanoseconds % 1_000_000 };
}

/** Parse the REST API's 2026-03-02T09:00:00.000+0000 form or RFC 3339, as the Go connector's layouts do. */
export function parseSalesforceDateTime(value: string): Instant | undefined {
  const match = salesforceDateTime.exec(value);
  // Go's two REST layouts take exactly three fraction digits before a +hhmm offset.
  if (match && match[8] !== 'Z' && !match[8]!.includes(':') && (match[7] ?? '').length !== 3) return undefined;
  return parseInstant(match);
}

/** Parse value as Go's time.Parse(time.RFC3339) does. */
export function parseRFC3339(value: string): Instant | undefined { return parseInstant(rfc3339.exec(value)); }

/** Compare two instants. */
export function compareInstants(left: Instant, right: Instant): number {
  return left.milliseconds - right.milliseconds || left.nanoseconds - right.nanoseconds;
}

/** Format a whole-second time as Go's time.RFC3339 does in UTC. */
export function formatRFC3339Seconds(milliseconds: number): string {
  return new Date(Math.floor(milliseconds / 1000) * 1000).toISOString().slice(0, 19) + 'Z';
}

/** Format a UTC time as Go's time.Time MarshalJSON does: RFC 3339 with trailing fraction zeros trimmed. */
export function formatGoTime(instant: Instant): string {
  const fraction = (String(((instant.milliseconds % 1000) + 1000) % 1000).padStart(3, '0') + String(instant.nanoseconds).padStart(6, '0')).replace(/0+$/, '');
  return formatRFC3339Seconds(instant.milliseconds).slice(0, 19) + (fraction ? '.' + fraction : '') + 'Z';
}

/** A field's text when it holds a JSON string; names match exactly, else without case, as Go's StringField. */
export function stringField(record: SalesforceRecord, fieldName: string): string | undefined {
  const fields = record.fields ?? {};
  const value = Object.hasOwn(fields, fieldName) ? fields[fieldName]
    : Object.entries(fields).find(([name]) => name.toLowerCase() === fieldName.toLowerCase())?.[1];
  return typeof value === 'string' ? value : undefined;
}

/** Compare CreatedDate with LastModifiedDate; an unreadable pair counts as updated. */
export function recordChangeKind(record: SalesforceRecord): RecordChangeKind {
  const createdText = stringField(record, 'CreatedDate');
  const modifiedText = stringField(record, 'LastModifiedDate');
  if (createdText === undefined || modifiedText === undefined) return RecordChangeUpdated;
  const created = parseSalesforceDateTime(createdText);
  const modified = parseSalesforceDateTime(modifiedText);
  return created && modified && compareInstants(created, modified) === 0 ? RecordChangeCreated : RecordChangeUpdated;
}

/** The Trigger event ID, a pure function of the record and its SystemModstamp, as the Go connector computes it. */
export function recordModifiedEventID(sObjectType: string, recordID: string, modstampMilliseconds: number): string {
  const digest = createHash('sha256').update([sObjectType, recordID, formatRFC3339Seconds(modstampMilliseconds)].join('\u0000')).digest();
  return `sfdc-${digest.subarray(0, 16).toString('hex')}`;
}

/** Compare IDs by their case-sensitive 15-character prefix, which both ID forms share. */
export function isSameRecordID(left: string, right: string): boolean {
  return isRecordID(left) && isRecordID(right) && left.slice(0, 15) === right.slice(0, 15);
}

/**
 * The committed cursor: every record ordered at or before (systemModstamp, recordId) was read. A blank recordId
 * means every record stamped at or before systemModstamp was read.
 */
export interface RecordPollCursor { sObjectType: string; systemModstamp: string; recordId: string }

/** Encode the cursor as Go's json.Marshal does: field order, Go's HTML escaping, and an omitted empty recordId. */
export function encodeRecordPollCursor(position: RecordPollCursor): Cursor {
  let token = `{"sObjectType":${goJSONString(position.sObjectType)},"systemModstamp":${goJSONString(position.systemModstamp)}`;
  if (position.recordId) token += `,"recordId":${goJSONString(position.recordId)}`;
  return { token: token + '}' };
}

/** Decode an untrusted stored cursor with Go's rules and return it with its time; throws when it is unusable. */
export function decodeRecordPollCursor(token: string): { position: RecordPollCursor; after: Instant } {
  const position: RecordPollCursor = { sObjectType: '', systemModstamp: '', recordId: '' };
  let decoded: unknown;
  try { decoded = JSON.parse(token); } catch { throw new Error('Salesforce poll cursor is not decodable'); }
  if (decoded !== null) {
    if (typeof decoded !== 'object' || Array.isArray(decoded)) throw new Error('Salesforce poll cursor is not decodable');
    for (const [key, value] of Object.entries(decoded)) {
      // Go matches a member exactly first, then without case; null leaves the field unchanged.
      const name = cursorFieldNames.find(item => item === key) ?? cursorFieldNames.find(item => item.toLowerCase() === key.toLowerCase());
      if (!name || value === null) continue;
      if (typeof value !== 'string') throw new Error('Salesforce poll cursor is not decodable');
      position[name] = value;
    }
  }
  const after = parseRFC3339(position.systemModstamp);
  if (!after || (position.recordId && !isRecordID(position.recordId))) throw new Error('Salesforce poll cursor is not decodable');
  return { position, after };
}

function goJSONString(value: string): string {
  return JSON.stringify(value).replace(/</g, '\\u003c').replace(/>/g, '\\u003e').replace(/&/g, '\\u0026')
    .replace(/\u2028/g, '\\u2028').replace(/\u2029/g, '\\u2029');
}
