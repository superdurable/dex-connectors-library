// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Comment, Contact, Conversation, Message, Recipient, Teammate } from './generated/models.js';

type Document = Record<string, unknown>;
export const identifiers = {
  conversation: /^cnv_[A-Za-z0-9]{1,40}$/, teammate: /^tea_[A-Za-z0-9]{1,40}$/,
  tag: /^tag_[A-Za-z0-9]{1,40}$/, inbox: /^inb_[A-Za-z0-9]{1,40}$/,
  status: /^sts_[A-Za-z0-9]{1,40}$/, contact: /^crd_[A-Za-z0-9]{1,40}$/,
  message: /^msg_[A-Za-z0-9]{1,40}$/, comment: /^com_[A-Za-z0-9]{1,40}$/,
  pageToken: /^[A-Za-z0-9._~-]{1,1000}$/,
};

export function object(value: unknown): Document { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('expected an object'); return value as Document; }
export function string(value: unknown): string { if (value == null) return ''; if (typeof value !== 'string') throw new Error('expected a string'); return value; }
export function array(value: unknown): unknown[] { if (value == null) return []; if (!Array.isArray(value)) throw new Error('expected a list'); return value; }
export function decode(body: Uint8Array): Document { return object(JSON.parse(Buffer.from(body).toString('utf8')) as unknown); }
export function requireID(label: string, value: string, pattern: RegExp): void { if (!pattern.test(value)) throw new Error(`${label} must be a Front ID`); }

export function timestamp(value: unknown): string {
  if (value == null) return '0001-01-01T00:00:00Z';
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0 || value > 1e11) throw new Error('a Front timestamp is not Unix seconds');
  if (value === 0) return '0001-01-01T00:00:00Z';
  return new Date(Math.round(value * 1000)).toISOString().replace(/\.000Z$/, 'Z');
}
function optionalTimestamp(value: unknown): string | undefined { const time = timestamp(value); return time.startsWith('0001-') ? undefined : time; }
function optional<T>(key: string, value: T | undefined | null): Record<string, T> { return value === undefined || value === null || value === '' ? {} : { [key]: value }; }
function list<T>(key: string, values: T[]): Record<string, T[]> { return values.length ? { [key]: values } : {}; }

export function teammate(value: unknown): Teammate | undefined {
  if (value == null) return undefined;
  const wire = object(value);
  if (!wire.id && !wire.email) return undefined;
  requireID('teammate', string(wire.id), identifiers.teammate);
  return { id: string(wire.id), ...optional('email', string(wire.email)), ...optional('firstName', string(wire.first_name)), ...optional('lastName', string(wire.last_name)) };
}
export function recipient(value: unknown): Recipient {
  const wire = object(value);
  const related = object(object(wire._links ?? {}).related ?? {});
  let contactId = '';
  try { const last = new URL(string(related.contact), 'https://api2.frontapp.com').pathname.split('/').at(-1) ?? ''; if (identifiers.contact.test(last)) contactId = last; } catch { /* A malformed optional link has no contact ID. */ }
  return { handle: string(wire.handle), ...optional('role', string(wire.role)), ...optional('name', string(wire.name)), ...optional('contactId', contactId) };
}
export function conversation(wire: Document): Conversation {
  requireID('conversation', string(wire.id), identifiers.conversation);
  if (!wire.status) throw new Error('a conversation lacks a status');
  const statusId = string(wire.status_id);
  if (statusId) requireID('status', statusId, identifiers.status);
  const tags = array(wire.tags).map(value => { const tag = object(value); requireID('tag', string(tag.id), identifiers.tag); return { id: string(tag.id), ...optional('name', string(tag.name)) }; });
  const assignee = teammate(wire.assignee);
  const mainRecipient = wire.recipient == null || !string(object(wire.recipient).handle) ? undefined : recipient(wire.recipient);
  return {
    id: string(wire.id), status: string(wire.status), createdAt: timestamp(wire.created_at),
    ...optional('type', string(wire.type)), ...optional('subject', string(wire.subject)), ...optional('statusId', statusId),
    ...optional('statusCategory', string(wire.status_category)), ...list('ticketIds', array(wire.ticket_ids).map(string)),
    ...(assignee ? { assignee } : {}), ...(mainRecipient ? { recipient: mainRecipient } : {}), ...list('tags', tags),
    ...(wire.is_private ? { isPrivate: true } : {}), ...(string(wire.status) === 'archived' && array(wire.scheduled_reminders).length ? { isSnoozed: true } : {}),
    ...optional('updatedAt', optionalTimestamp(wire.updated_at)), ...optional('waitingSince', optionalTimestamp(wire.waiting_since)), ...optional('dueAt', optionalTimestamp(wire.due_at)),
  };
}
export function page(body: Uint8Array): { results: Document[]; next: unknown; total: unknown } {
  const wire = decode(body);
  if (!Array.isArray(wire._results)) throw new Error('the page has no list');
  return { results: wire._results.map(object), next: object(wire._pagination ?? {}).next, total: wire._total };
}
function truncate(value: string): { body: string; isBodyTruncated?: true } {
  const bytes = Buffer.from(value);
  if (bytes.length <= 16_384) return { body: value };
  let cut = 16_384;
  while (cut > 0 && (bytes[cut]! & 0xc0) === 0x80) cut--;
  return { body: bytes.subarray(0, cut).toString('utf8'), isBodyTruncated: true };
}
export function message(wire: Document): Message {
  requireID('message', string(wire.id), identifiers.message);
  const author = teammate(wire.author);
  const recipients = array(wire.recipients).map(recipient);
  return { id: string(wire.id), isInbound: Boolean(wire.is_inbound), createdAt: timestamp(wire.created_at),
    ...optional('type', string(wire.type)), ...(wire.draft_mode ? { isDraft: true } : {}), ...optional('subject', string(wire.subject)),
    ...optional('blurb', string(wire.blurb)), ...(string(wire.body) ? truncate(string(wire.body)) : {}), ...(author ? { author } : {}), ...list('recipients', recipients) };
}
export function comment(wire: Document): Comment {
  requireID('comment', string(wire.id), identifiers.comment);
  const author = teammate(wire.author);
  return { id: string(wire.id), postedAt: timestamp(wire.posted_at), ...(string(wire.body) ? truncate(string(wire.body)) : {}),
    ...(author ? { author } : {}), ...(wire.is_pinned ? { isPinned: true } : {}) };
}
export function contact(wire: Document, email: string): Contact {
  requireID('contact', string(wire.id), identifiers.contact);
  const handles = array(wire.handles).map(value => ({ handle: string(object(value).handle), source: string(object(value).source) }));
  if (!handles.some(handle => handle.source === 'email' && handle.handle.toLowerCase() === email.toLowerCase())) throw new Error('the contact does not hold the requested email handle');
  const listNames = array(wire.lists).map(value => string(object(value).name)).filter(Boolean);
  return { id: string(wire.id), ...optional('name', string(wire.name)), ...optional('description', string(wire.description)), ...list('handles', handles),
    ...list('listNames', listNames), ...(wire.is_private ? { isPrivate: true } : {}), ...optional('updatedAt', optionalTimestamp(wire.updated_at)) };
}
