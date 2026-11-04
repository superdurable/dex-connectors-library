// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Customer, Ticket, TicketMessage, TicketDetails, GetTicketInput, CustomerMatches, SearchTicketsInput, SearchTicketsOutput } from './generated/models.js';

type Wire = Record<string, unknown>;

/** Parse one JSON object without accepting arrays or null. */
export function objectBody(body: Uint8Array): Wire { return asObject(JSON.parse(Buffer.from(body).toString('utf8')) as unknown); }
function asObject(value: unknown): Wire { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('expected a Gorgias object'); return value as Wire; }
function stringValue(value: unknown): string { return typeof value === 'string' ? value : ''; }
function positiveID(value: unknown): number { if (!Number.isSafeInteger(value) || Number(value) < 1) throw new TypeError('missing positive ID'); return Number(value); }
function optionalID(value: unknown): number { return value === undefined || value === null ? 0 : Number(value); }
function time(value: unknown): string {
  if (typeof value !== 'string') throw new TypeError('invalid Gorgias timestamp');
  const parsed = /^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)?$/.exec(value);
  if (!parsed) throw new TypeError('invalid Gorgias timestamp');
  const instant = new Date(`${parsed[1]}${parsed[3] ?? 'Z'}`);
  if (Number.isNaN(instant.getTime())) throw new TypeError('invalid Gorgias timestamp');
  const fraction = parsed[2]?.replace(/0+$/, '') ?? '';
  return `${instant.toISOString().slice(0, 19)}${fraction ? `.${fraction}` : ''}Z`;
}
function optionalTime(value: unknown): string | undefined { return value === undefined || value === null || value === '' ? undefined : time(value); }
function nonemptyString(value: unknown): string { if (typeof value !== 'string' || !value) throw new TypeError('missing string'); return value; }

/** Decode one Gorgias ticket, preserving optional Go JSON fields. */
export function decodeTicket(wire: Wire): Ticket {
  const id = positiveID(wire.id);
  const status = nonemptyString(wire.status);
  const createdAt = time(wire.created_datetime);
  const updatedAt = optionalTime(wire.updated_datetime) ?? createdAt;
  const tags = wire.tags === undefined || wire.tags === null ? [] : (wire.tags as unknown[]).map(tag => nonemptyString(asObject(tag).name));
  const customer = wire.customer ? asObject(wire.customer) : undefined;
  const assigneeUser = wire.assignee_user ? asObject(wire.assignee_user) : undefined;
  const assigneeTeam = wire.assignee_team ? asObject(wire.assignee_team) : undefined;
  return {
    id, status, createdAt, updatedAt,
    ...(stringValue(wire.subject) ? { subject: stringValue(wire.subject) } : {}),
    ...(stringValue(wire.priority) ? { priority: stringValue(wire.priority) } : {}),
    ...(stringValue(wire.channel) ? { channel: stringValue(wire.channel) } : {}),
    ...(stringValue(wire.via) ? { via: stringValue(wire.via) } : {}),
    ...(stringValue(wire.external_id) ? { externalId: stringValue(wire.external_id) } : {}),
    ...(stringValue(wire.language) ? { language: stringValue(wire.language) } : {}),
    ...(wire.spam === true ? { isSpam: true } : {}),
    ...(optionalID(customer?.id) ? { requesterId: optionalID(customer?.id) } : {}),
    ...(stringValue(customer?.email) ? { requesterEmail: stringValue(customer?.email) } : {}),
    ...(optionalID(assigneeUser?.id) ? { assigneeUserId: optionalID(assigneeUser?.id) } : {}),
    ...(optionalID(assigneeTeam?.id) ? { assigneeTeamId: optionalID(assigneeTeam?.id) } : {}),
    ...(tags.length ? { tags } : {}),
    ...(Number(wire.messages_count) ? { messageCount: Number(wire.messages_count) } : {}),
    ...(optionalTime(wire.last_received_message_datetime) ? { lastReceivedMessageAt: optionalTime(wire.last_received_message_datetime) } : {}),
    ...(optionalTime(wire.last_message_datetime) ? { lastMessageAt: optionalTime(wire.last_message_datetime) } : {}),
    ...(optionalTime(wire.closed_datetime) ? { closedAt: optionalTime(wire.closed_datetime) } : {}),
    ...(optionalTime(wire.snooze_datetime) ? { snoozedUntil: optionalTime(wire.snooze_datetime) } : {}),
    ...(optionalTime(wire.trashed_datetime) ? { trashedAt: optionalTime(wire.trashed_datetime) } : {}),
  };
}

/** Decode one customer. */
export function decodeCustomer(wire: Wire): Customer {
  return { id: positiveID(wire.id), ...(stringValue(wire.email) ? { email: stringValue(wire.email) } : {}), ...(stringValue(wire.name) ? { name: stringValue(wire.name) } : {}), ...(stringValue(wire.external_id) ? { externalId: stringValue(wire.external_id) } : {}), ...(stringValue(wire.language) ? { language: stringValue(wire.language) } : {}) };
}

/** Decode one ticket message with a bounded plain-text body. */
export function decodeMessage(wire: Wire): TicketMessage {
  const bodyText = stringValue(wire.body_text) || stringValue(wire.stripped_text);
  let body = bodyText;
  let isBodyTruncated = false;
  if (Buffer.byteLength(body, 'utf8') > 16_384) {
    const bytes = Buffer.from(body, 'utf8');
    body = bytes.subarray(0, 16_384).toString('utf8').replace(/\uFFFD$/, '');
    isBodyTruncated = true;
  }
  const sender = wire.sender ? asObject(wire.sender) : undefined;
  const channel = nonemptyString(wire.channel);
  return {
    id: positiveID(wire.id), ticketId: optionalID(wire.ticket_id), channel, body,
    isPublic: channel !== 'internal-note' && wire.public !== false, isFromAgent: wire.from_agent === true,
    createdAt: time(wire.created_datetime),
    ...(stringValue(wire.via) ? { via: stringValue(wire.via) } : {}),
    ...(optionalID(sender?.id) ? { senderId: optionalID(sender?.id) } : {}),
    ...(stringValue(wire.subject) ? { subject: stringValue(wire.subject) } : {}),
    ...(isBodyTruncated ? { isBodyTruncated: true } : {}),
    ...(stringValue(wire.external_id) ? { externalId: stringValue(wire.external_id) } : {}),
    ...(optionalTime(wire.sent_datetime) ? { sentAt: optionalTime(wire.sent_datetime) } : {}),
    ...(optionalTime(wire.failed_datetime) ? { failedAt: optionalTime(wire.failed_datetime) } : {}),
  };
}

/** Decode a cursor list envelope. */
export function decodeList(body: Uint8Array): { data: Wire[]; nextCursor: string } {
  const wire = objectBody(body);
  if (!Array.isArray(wire.data)) throw new TypeError('page has no data array');
  const nextCursor = wire.meta ? stringValue(asObject(wire.meta).next_cursor) : '';
  if (nextCursor && (nextCursor.length > 2048 || !/^[A-Za-z0-9+/=_.:-]+$/.test(nextCursor))) throw new TypeError('invalid cursor');
  return { data: wire.data.map(asObject), nextCursor };
}

/** Decode a customer lookup, including Gorgias's bare-array form. */
export function decodeCustomerMatches(body: Uint8Array, email: string): CustomerMatches {
  const parsed = JSON.parse(Buffer.from(body).toString('utf8')) as unknown;
  const page = Array.isArray(parsed) ? { data: parsed.map(asObject), nextCursor: '' } : decodeList(body);
  const customers = page.data.map(decodeCustomer).filter(customer => customer.email?.toLowerCase() === email.toLowerCase());
  return { email, customers, ...(page.nextCursor ? { hasMore: true } : {}) };
}

/** Decode ticket detail and order its messages newest first. */
export function decodeDetails(body: Uint8Array, input: GetTicketInput): TicketDetails {
  const wire = objectBody(body);
  const ticket = decodeTicket(wire);
  if (ticket.id !== input.ticketId) throw new TypeError('ticket response is for another ticket');
  const messages = (Array.isArray(wire.messages) ? wire.messages : []).map(value => decodeMessage(asObject(value)));
  for (const message of messages) {
    if (message.ticketId && message.ticketId !== ticket.id) throw new TypeError('message belongs to another ticket');
    message.ticketId = ticket.id;
  }
  messages.sort((left, right) => right.createdAt.localeCompare(left.createdAt) || right.id - left.id);
  const limit = input.latestMessageLimit || 5;
  return { ticket, latestMessages: messages.slice(0, limit), ...(wire.customer ? { requester: decodeCustomer(asObject(wire.customer)) } : {}), ...(messages.length > limit ? { hasOlderMessages: true } : {}) };
}

/** Decode one ticket page and apply local filters. */
export function decodeTicketPage(body: Uint8Array, input: SearchTicketsInput): SearchTicketsOutput {
  const page = decodeList(body);
  if (page.data.length > 100) throw new TypeError('ticket page is too large');
  let nextCursor = page.nextCursor;
  const tickets: Ticket[] = [];
  for (const wire of page.data) {
    const ticket = decodeTicket(wire);
    if (input.updatedSince && Date.parse(ticket.updatedAt) < Date.parse(input.updatedSince)) {
      if (!input.viewId) nextCursor = '';
      continue;
    }
    if (input.statuses?.length && !input.statuses.includes(ticket.status)) continue;
    if (input.priorities?.length && !input.priorities.includes(ticket.priority ?? '')) continue;
    if (input.tags?.length && !input.tags.some(tag => ticket.tags?.includes(tag))) continue;
    tickets.push(ticket);
  }
  return { tickets, listedCount: page.data.length, ...(nextCursor ? { nextCursor } : {}) };
}
