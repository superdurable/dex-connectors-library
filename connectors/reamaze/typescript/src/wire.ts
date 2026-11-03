// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Contact, Conversation, ConversationMessage } from './generated/models.js';

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('expected object');
  return value as Record<string, unknown>;
}
function text(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new Error('expected string'); return value; }
function integer(value: unknown, defaultValue = 0): number { if (value === undefined || value === null) return defaultValue; if (!Number.isSafeInteger(value)) throw new Error('expected integer'); return value as number; }
function optionalObject(value: unknown): Record<string, unknown> | undefined { return value === undefined || value === null ? undefined : object(value); }
function strings(value: unknown): string[] { if (value === undefined || value === null) return []; if (!Array.isArray(value)) throw new Error('expected string array'); return value.map(text); }
function participant(value: unknown) { const wire = optionalObject(value); if (!wire) return undefined; const name = text(wire.name); const email = text(wire.email); return name || email ? { ...(name ? { name } : {}), ...(email ? { email } : {}) } : undefined; }
function truncate(value: string): [string, boolean] {
  const encoded = Buffer.from(value);
  if (encoded.length <= 16_384) return [value, false];
  return [new TextDecoder('utf-8', { fatal: true }).decode(encoded.subarray(0, 16_384).subarray(0, utf8Boundary(encoded, 16_384))), true];
}
function utf8Boundary(bytes: Uint8Array, length: number): number { while (length > 0 && (bytes[length]! & 0xc0) === 0x80) length--; return length; }
/** Parse an RFC 3339 instant and return Go's UTC JSON timestamp shape. */
export function parseInstant(value: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) throw new Error('invalid instant');
  const [, year, month, day, hour, minute, second, fraction, offset] = match;
  const y = Number(year), m = Number(month), d = Number(day), h = Number(hour), min = Number(minute), s = Number(second);
  const endOfMonth = new Date(0);
  endOfMonth.setUTCFullYear(y, m, 0);
  if (m < 1 || m > 12 || d < 1 || d > endOfMonth.getUTCDate() || h > 23 || min > 59 || s > 59) throw new Error('invalid instant');
  const offsetHours = offset === 'Z' ? 0 : Number(offset!.slice(1, 3));
  const offsetMinutes = offset === 'Z' ? 0 : Number(offset!.slice(4));
  if (offsetHours > 24 || offsetMinutes > 60) throw new Error('invalid instant');
  const signedOffset = offset === 'Z' ? 0 : (offset!.startsWith('+') ? 1 : -1) * (offsetHours * 60 + offsetMinutes);
  const date = new Date(0);
  date.setUTCFullYear(y, m - 1, d);
  date.setUTCHours(h, min, s, 0);
  const timestamp = date.getTime() - signedOffset * 60_000;
  if (!Number.isFinite(timestamp)) throw new Error('invalid instant');
  const whole = new Date(timestamp).toISOString().replace(/\.000Z$/, 'Z');
  const nanoseconds = fraction?.slice(0, 9).replace(/0+$/, '');
  return nanoseconds ? whole.slice(0, -1) + '.' + nanoseconds + 'Z' : whole;
}
function optionalInstant(value: unknown): string | undefined { const wire = optionalObject(value); if (wire) text(wire.body); const raw = wire ? text(wire.created_at) : ''; return raw ? parseInstant(raw) : undefined; }
/** Check declared fields before a conversation is considered for reconciliation. */
export function validateConversationWire(value: unknown): Record<string, unknown> {
  const wire = value === null ? {} : object(value);
  text(wire.slug); text(wire.subject); integer(wire.status); text(wire.created_at); strings(wire.tag_list);
  for (const field of ['message', 'last_customer_message', 'last_staff_message']) {
    const message = optionalObject(wire[field]);
    if (message) { text(message.body); text(message.created_at); }
  }
  participant(wire.author); participant(wire.assignee);
  const category = optionalObject(wire.category);
  if (category) { text(category.name); text(category.slug); integer(category.channel); }
  const data = optionalObject(wire.data);
  return { ...wire, ...(data ? { data } : {}) };
}
/** Decode a provider conversation with Go's required-field checks. */
export function decodeConversation(value: unknown, includeFirstMessage: boolean, expectedId = ''): Conversation {
  const wire = validateConversationWire(value);
  const id = text(wire.slug);
  if (!/^[A-Za-z0-9][A-Za-z0-9_-]{0,254}$/.test(id) || (expectedId && expectedId !== id)) throw new Error('conversation slug is missing or invalid');
  if (wire.status === undefined || wire.status === null) throw new Error('conversation status is missing');
  const status = integer(wire.status);
  const createdAt = parseInstant(text(wire.created_at));
  const channelWire = optionalObject(wire.category);
  const channel = channelWire ? { slug: text(channelWire.slug), ...(text(channelWire.name) ? { name: text(channelWire.name) } : {}), ...(integer(channelWire.channel) ? { type: integer(channelWire.channel) } : {}) } : { slug: '' };
  const requester = participant(wire.author);
  const assignee = participant(wire.assignee);
  const message = optionalObject(wire.message);
  const messageBody = message ? text(message.body) : '';
  if (message) text(message.created_at);
  const [firstMessage, isFirstMessageTruncated] = includeFirstMessage ? truncate(messageBody) : ['', false];
  const lastCustomerMessageAt = optionalInstant(wire.last_customer_message);
  const lastStaffMessageAt = optionalInstant(wire.last_staff_message);
  return { id, ...(text(wire.subject) ? { subject: text(wire.subject) } : {}), status, tags: strings(wire.tag_list), channel,
    ...(requester ? { requester } : {}), ...(assignee ? { assignee } : {}), ...(firstMessage ? { firstMessage } : {}),
    ...(isFirstMessageTruncated ? { isFirstMessageTruncated } : {}), createdAt,
    ...(lastCustomerMessageAt ? { lastCustomerMessageAt } : {}), ...(lastStaffMessageAt ? { lastStaffMessageAt } : {}) };
}
/** Decode one provider message. */
export function decodeMessage(value: unknown): ConversationMessage {
  const wire = object(value);
  if (wire.visibility === undefined || wire.visibility === null) throw new Error('message visibility is missing');
  const visibility = integer(wire.visibility);
  const origin = integer(wire.origin);
  const originIdRaw = wire.origin_id;
  if (originIdRaw !== undefined && originIdRaw !== null && typeof originIdRaw !== 'string' && typeof originIdRaw !== 'number') throw new Error('invalid origin id');
  const originId = originIdRaw === undefined || originIdRaw === null ? '' : String(originIdRaw);
  if (Buffer.byteLength(originId) > 256) throw new Error('invalid origin id');
  const [body, isBodyTruncated] = truncate(text(wire.body));
  const author = participant(wire.user);
  return { body, ...(isBodyTruncated ? { isBodyTruncated } : {}), visibility, isInternalNote: visibility === 1, origin,
    ...(originId ? { originId } : {}), ...(author ? { author } : {}), createdAt: parseInstant(text(wire.created_at)) };
}
/** Parse an HTTP JSON body. */
export function parseBody(body: Uint8Array): Record<string, unknown> {
  return object(JSON.parse(Buffer.from(body).toString('utf8'), (key: string, value: unknown, context?: { source: string }) => {
    if (key !== 'origin_id' || typeof value !== 'number') return value;
    if (!context?.source) throw new Error('numeric origin id source is unavailable');
    return context.source;
  }) as unknown);
}
/** Check one bounded Re:amaze list envelope. */
export function page(wire: Record<string, unknown>, entries: unknown): { pageCount: number; totalCount: number; values: unknown[] } {
  if (!Array.isArray(entries)) throw new Error('response is not a page');
  if (wire.page_count === undefined || wire.page_count === null || wire.total_count === undefined || wire.total_count === null) throw new Error('page counters missing');
  const pageCount = integer(wire.page_count), totalCount = integer(wire.total_count), pageSize = wire.page_size === undefined || wire.page_size === null ? 100 : integer(wire.page_size);
  if (pageCount < 0 || totalCount < 0 || entries.length > 100 || entries.length > Math.max(pageSize, 0)) throw new Error('invalid page counters');
  return { pageCount, totalCount, values: entries };
}
/** Decode the first page of a contact search, keeping exact email matches. */
export function matchingContacts(wire: Record<string, unknown>, email: string): { contacts: Contact[]; hasMoreCandidates?: boolean } {
  const listing = page(wire, wire.contacts);
  const contacts = listing.values.map(value => value === null ? {} : object(value)).filter(contact => {
    text(contact.name); text(contact.friendly_name);
    return text(contact.email).toLowerCase() === email.toLowerCase();
  }).map(contact => ({
    ...(text(contact.name) ? { name: text(contact.name) } : {}), email: text(contact.email), ...(text(contact.friendly_name) ? { friendlyName: text(contact.friendly_name) } : {}),
  }));
  return { contacts, ...(listing.pageCount > 1 ? { hasMoreCandidates: true } : {}) };
}
/** Encode Go url.PathEscape's path-segment allowlist. */
export function pathEscape(value: string): string { return encodeURIComponent(value).replace(/%24/g, '$').replace(/%26/g, '&').replace(/%2B/g, '+').replace(/%3D/g, '=').replace(/%3A/g, ':').replace(/%40/g, '@').replace(/[!'()*]/g, character => '%' + character.charCodeAt(0).toString(16).toUpperCase()); }
/** Convert URLSearchParams to Go url.Values.Encode ordering and escaping. */
export function encodeQuery(values: Record<string, string>): URLSearchParams {
  const params = new URLSearchParams();
  for (const key of Object.keys(values).sort()) params.set(key, values[key]!);
  return params;
}
