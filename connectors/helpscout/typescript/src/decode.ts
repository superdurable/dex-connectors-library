// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Conversation, Customer, Person, Thread } from './generated/models.js';

/** Read a non-array JSON object; Go treats absent fields as their zero values. */
export function parseObject(body: Uint8Array): Record<string, unknown> {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  return asObject(value);
}

/** Require an object and reject wire types Go's JSON decoder rejects. */
export function asObject(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Help Scout returned a malformed response');
  return value as Record<string, unknown>;
}

/** Read a Go string field, preserving empty defaults. */
export function wireString(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value !== 'string') throw new TypeError('Help Scout returned a wrong-typed string');
  return value;
}

/** Read a Go integer field, preserving zero defaults. */
export function wireInteger(value: unknown): number {
  if (value === null || value === undefined) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('Help Scout returned a wrong-typed integer');
  return value;
}

/** Read a Go array field, preserving nil as empty. */
export function wireArray(value: unknown): unknown[] {
  if (value === null || value === undefined) return [];
  if (!Array.isArray(value)) throw new TypeError('Help Scout returned a wrong-typed array');
  return value;
}

/** Decode the safe person subset. */
export function decodePerson(value: unknown): Person | undefined {
  if (value === null || value === undefined) return undefined;
  const person = asObject(value);
  const id = wireInteger(person.id);
  const type = wireString(person.type), email = wireString(person.email), first = wireString(person.first), last = wireString(person.last);
  if (id < 1) return undefined;
  return { id, ...(type ? { type } : {}), ...(email ? { email } : {}), ...(first ? { firstName: first } : {}), ...(last ? { lastName: last } : {}) };
}

/** Decode the safe conversation subset and retain Help Scout status values. */
export function decodeConversation(value: unknown, expectedId = 0): Conversation {
  const wire = asObject(value);
  const id = wireInteger(wire.id);
  if (id < 1 || (expectedId && id !== expectedId)) throw new TypeError('conversation has no matching ID');
  const source = optionalObject(wire.source);
  const links = optionalObject(wire._links);
  const web = optionalObject(links.web);
  const tags = wireArray(wire.tags).map(item => wireString(item === null ? undefined : asObject(item).tag)).filter(Boolean);
  const waiting = optionalObject(wire.customerWaitingSince);
  const preview = truncateUTF8(wireString(wire.preview), 1024);
  return {
    id, number: wireInteger(wire.number), subject: wireString(wire.subject), status: wireString(wire.status),
    mailboxId: wireInteger(wire.mailboxId), threadCount: wireInteger(wire.threads), tags,
    ...optionalString('state', wire.state), ...optionalString('type', wire.type),
    ...optionalNumber('folderId', wire.folderId), ...(preview ? { preview } : {}),
    ...(decodePerson(wire.assignee) ? { assignee: decodePerson(wire.assignee)! } : {}),
    ...(decodePerson(wire.primaryCustomer) ? { primaryCustomer: decodePerson(wire.primaryCustomer)! } : {}),
    ...(decodePerson(wire.createdBy) ? { createdBy: decodePerson(wire.createdBy)! } : {}),
    ...optionalString('sourceType', source.type), ...optionalString('sourceVia', source.via),
    ...optionalTimestamp('createdAt', wire.createdAt), ...optionalTimestamp('closedAt', wire.closedAt),
    ...optionalTimestamp('userUpdatedAt', wire.userUpdatedAt), ...optionalTimestamp('customerWaitingSince', waiting.time),
    ...optionalHTTPSURL(wireString(web.href)),
  };
}

/** Decode a newest-first thread. */
export function decodeThread(value: unknown): Thread {
  const wire = asObject(value);
  const id = wireInteger(wire.id);
  const type = wireString(wire.type);
  if (id < 1 || !type) throw new TypeError('thread has no ID or type');
  const source = optionalObject(wire.source);
  const rawBody = wireString(wire.body);
  const body = truncateUTF8(rawBody, 16_384);
  return { id, type, createdAt: parseTimestamp(wireString(wire.createdAt)),
    ...optionalString('status', wire.status), ...optionalString('state', wire.state),
    ...(body ? { body } : {}), ...(Buffer.byteLength(body) < Buffer.byteLength(rawBody) ? { isBodyTruncated: true } : {}),
    ...(decodePerson(wire.createdBy) ? { createdBy: decodePerson(wire.createdBy)! } : {}),
    ...optionalString('sourceType', source.type), ...optionalString('sourceVia', source.via),
  };
}

/** Decode one customer profile from a V3 customer search page. */
export function decodeCustomer(value: unknown): Customer {
  const wire = asObject(value);
  const id = wireInteger(wire.id);
  if (id < 1) throw new TypeError('customer has no ID');
  const embedded = optionalObject(wire._embedded);
  const emails = wireArray(embedded.emails).map(item => wireString(asObject(item).value)).filter(Boolean);
  return { id, emails, conversationCount: wireInteger(wire.conversationCount), createdAt: parseTimestamp(wireString(wire.createdAt)),
    ...optionalString('firstName', wire.firstName), ...optionalString('lastName', wire.lastName),
    ...optionalNumber('organizationId', wire.organizationId), ...optionalTimestamp('updatedAt', wire.updatedAt),
  };
}

/** Decode an RFC 3339 instant with Go's civil-date and offset rules. */
export function parseTimestamp(value: string): string {
  const match = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:[.,](\d+))?(Z|[+-]\d\d:\d\d)$/.exec(value);
  if (!match) throw new TypeError('timestamp is not ISO 8601');
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number);
  if (year === undefined || month === undefined || day === undefined || hour === undefined || minute === undefined || second === undefined) throw new TypeError('timestamp is invalid');
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const monthLength = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1] ?? 0;
  if (month < 1 || month > 12 || day < 1 || day > monthLength || hour > 23 || minute > 59 || second > 59) throw new TypeError('timestamp has an invalid civil date');
  const offset = match[8] ?? 'Z';
  const offsetMinutes = offset === 'Z' ? 0 : (offset[0] === '-' ? -1 : 1) * (Number(offset.slice(1, 3)) * 60 + Number(offset.slice(4, 6)));
  if (offset !== 'Z' && Number(offset.slice(4, 6)) > 59) throw new TypeError('timestamp has an invalid offset');
  const civil = new Date(0);
  civil.setUTCFullYear(year, month - 1, day);
  civil.setUTCHours(hour, minute, second, 0);
  const epoch = civil.getTime() - offsetMinutes * 60_000;
  const date = new Date(epoch);
  if (!Number.isFinite(epoch)) throw new TypeError('timestamp is invalid');
  const fractional = (match[7] ?? '').slice(0, 9).replace(/0+$/, '');
  return `${date.toISOString().slice(0, 19)}${fractional ? `.${fractional}` : ''}Z`;
}

/** Truncate on a UTF-8 boundary. */
export function truncateUTF8(value: string, bytes: number): string {
  const buffer = Buffer.from(value);
  if (buffer.length <= bytes) return value;
  return new TextDecoder('utf-8', { fatal: true }).decode(buffer.subarray(0, bytes).subarray(0, utf8Boundary(buffer, bytes)));
}

function utf8Boundary(buffer: Uint8Array, bytes: number): number {
  let cut = bytes;
  while (cut > 0 && (buffer[cut]! & 0xc0) === 0x80) cut--;
  return cut;
}

function optionalObject(value: unknown): Record<string, unknown> { return value === undefined || value === null ? {} : asObject(value); }
function optionalString(name: string, value: unknown): Record<string, string> { const text = wireString(value); return text ? { [name]: text } : {}; }
function optionalNumber(name: string, value: unknown): Record<string, number> { const number = wireInteger(value); return number ? { [name]: number } : {}; }
function optionalTimestamp(name: string, value: unknown): Record<string, string> { const text = wireString(value); return text ? { [name]: parseTimestamp(text) } : {}; }
function optionalHTTPSURL(value: string): Record<string, string> {
  try { const url = new URL(value); if (url.protocol === 'https:' && url.host && !url.username && !url.password) return { webUrl: value }; }
  catch { /* A malformed provider link is omitted, as in Go. */ }
  return {};
}
