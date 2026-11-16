// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { CRMObject, SearchObjectsInput } from './generated/models.js';
import type { ObjectType } from './operations.js';
import type { CRMObjectChangedTriggerConfiguration } from './crm_object_changed.js';

/** HubSpot's limit on results reachable through paging for one search. */
export const MaximumSearchResults = 10_000;
/** Records per change search page; one page of full records stays well under the default response limit. */
export const crmObjectChangePageSize = 100;

const rfc3339Pattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))$/;

/** The search property HubSpot updates on every change of an object type. */
export function lastModifiedProperty(objectType: ObjectType): string {
  return objectType === 'contacts' ? 'lastmodifieddate' : 'hs_lastmodifieddate';
}

/** Build the ascending last-modified search the Trigger repeats. */
export function newCRMObjectChangeSearch(configuration: CRMObjectChangedTriggerConfiguration, anchorMilliseconds: number, after: string): SearchObjectsInput {
  const modifiedProperty = lastModifiedProperty(configuration.objectType);
  return {
    objectType: configuration.objectType,
    filterGroups: [{ filters: [{ propertyName: modifiedProperty, operator: 'GTE', value: String(anchorMilliseconds) }] }],
    sort: { propertyName: modifiedProperty, direction: 'ASCENDING' },
    properties: [...(configuration.properties ?? []), 'createdate', modifiedProperty],
    limit: crmObjectChangePageSize,
    ...(after ? { after } : {}),
  };
}

/**
 * Parse an RFC 3339 time as Go's time.RFC3339Nano layout does and return its Unix milliseconds, rounded down as
 * Go's UnixMilli does; undefined for any other text.
 */
export function parseRFC3339Milliseconds(text: string): number | undefined {
  const match = rfc3339Pattern.exec(text);
  if (!match) return undefined;
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number) as [number, number, number, number, number, number];
  if (month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) || hour > 23 || minute > 59 || second > 59) return undefined;
  let offsetMinutes = 0;
  if (match[8] !== 'Z') {
    const offsetHours = Number(match[10]), offsetMinute = Number(match[11]);
    // Go accepts offsets of 24 hours or 60 minutes.
    if (offsetHours > 24 || offsetMinute > 60) return undefined;
    offsetMinutes = (match[9] === '-' ? -1 : 1) * (offsetHours * 60 + offsetMinute);
  }
  const date = new Date(0);
  date.setUTCFullYear(year, month - 1, day);
  date.setUTCHours(hour, minute, second, 0);
  // Go reads at most nine fraction digits and ignores the rest.
  const fractionMilliseconds = Math.floor(Number((match[7] ?? '').slice(0, 9).padEnd(9, '0') || '0') / 1_000_000);
  return date.getTime() - offsetMinutes * 60_000 + fractionMilliseconds;
}

/** Format Unix milliseconds as Go encodes the UTC time.Time: RFC 3339 with trailing fraction zeros removed. */
export function formatGoTimeMilliseconds(milliseconds: number): string {
  return new Date(milliseconds).toISOString().replace(/\.(\d*?)0*Z$/, (_whole, digits: string) => (digits ? `.${digits}Z` : 'Z'));
}

/** Read the last-modified property, falling back to the record's updatedAt. */
export function recordModifiedMilliseconds(object: CRMObject): number {
  const value = object.properties?.[lastModifiedProperty(object.objectType as ObjectType)];
  if (value !== undefined) {
    const parsed = parseRFC3339Milliseconds(value);
    if (parsed === undefined) throw new Error('HubSpot returned an invalid last-modified date');
    return parsed;
  }
  const updated = object.updatedAt ? parseRFC3339Milliseconds(object.updatedAt) : undefined;
  if (updated === undefined) throw new Error('HubSpot returned a record without a last-modified date');
  return updated;
}

/** The record's creation time in Unix milliseconds; a record without one counts as created before every change. */
export function recordCreatedMilliseconds(object: CRMObject): number {
  return (object.createdAt ? parseRFC3339Milliseconds(object.createdAt) : undefined) ?? Number.NEGATIVE_INFINITY;
}

function daysInMonth(year: number, month: number): number {
  if (month === 2) return (year % 4 === 0 && year % 100 !== 0) || year % 400 === 0 ? 29 : 28;
  return [4, 6, 9, 11].includes(month) ? 30 : 31;
}
