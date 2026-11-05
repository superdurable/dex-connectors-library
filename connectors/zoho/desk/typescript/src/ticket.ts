// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Ticket, TicketComment, TicketContact, TicketThread } from './generated/models.js';

type Wire = Record<string, unknown>;
const numericId = /^[0-9]{1,20}$/;

/** Decode a provider JSON object without coercing wrong-typed fields. */
export function decodeObject(body: Uint8Array): Wire {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  return object(value);
}
function object(value: unknown): Wire { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('response is not an object'); return value as Wire; }
function text(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new Error('field is not a string'); return value; }
function id(value: unknown): string {
  if (value === undefined || value === null) return '';
  const result = typeof value === 'string' ? value : typeof value === 'number' && Number.isSafeInteger(value) ? String(value) : '';
  if (result && numericId.test(result)) return result;
  if (result === '' && (value === '' || value === null || value === undefined)) return '';
  throw new Error('ID is not a numeric Zoho Desk ID');
}
function count(value: unknown): number {
  if (value === undefined || value === null || value === '') return 0;
  const result = typeof value === 'number' ? value : typeof value === 'string' && /^[0-9]+$/.test(value) ? Number(value) : NaN;
  if (!Number.isSafeInteger(result) || result < 0) throw new Error('count is not a non-negative integer');
  return result;
}
function flag(value: unknown): boolean {
  if (value === undefined || value === null || value === '' || value === false || value === 'false') return false;
  if (value === true || value === 'true') return true;
  throw new Error('flag is not a boolean');
}
function optionalString(fields: Wire, key: string, output: Wire, target = key): void { const value = text(fields[key]); if (value) output[target] = value; }
function optionalFlag(fields: Wire, key: string, output: Wire, target = key): void { const value = flag(fields[key]); if (value) output[target] = value; }
function optionalCount(fields: Wire, key: string, output: Wire, target = key): void { const value = count(fields[key]); if (value) output[target] = value; }
function optionalId(fields: Wire, key: string, output: Wire, target = key): void { const value = id(fields[key]); if (value) output[target] = value; }

/** Parse Go-accepted RFC 3339 time and emit UTC in the Go JSON spelling. */
export function timestamp(field: string, value: unknown): string {
  if (typeof value !== 'string') throw new Error(`${field} is not an ISO 8601 timestamp`);
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))$/.exec(value);
  if (!match) throw new Error(`${field} is not an ISO 8601 timestamp`);
  const year = Number(match[1]), month = Number(match[2]), day = Number(match[3]);
  const days = [31, year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]! || Number(match[4]) > 23 || Number(match[5]) > 59 || Number(match[6]) > 59 || Number(match[10]) > 99 || Number(match[11]) > 59) throw new Error(`${field} is not an ISO 8601 timestamp`);
  const offset = match[8] === 'Z' ? 0 : (match[9] === '+' ? 1 : -1) * (Number(match[10]) * 60 + Number(match[11]));
  if (Math.abs(offset) >= 1440) throw new Error(`${field} is not an ISO 8601 timestamp`);
  const milliseconds = Date.UTC(year, month - 1, day, Number(match[4]), Number(match[5]), Number(match[6])) - offset * 60_000;
  if (!Number.isFinite(milliseconds)) throw new Error(`${field} is not an ISO 8601 timestamp`);
  const date = new Date(milliseconds);
  const fraction = (match[7] ?? '').slice(0, 9).padEnd(9, '0');
  const prefix = date.toISOString().slice(0, 19);
  const digits = fraction.replace(/0+$/, '');
  return `${prefix}${digits ? '.' + digits : ''}Z`;
}
function truncate(value: string): { value: string; truncated: boolean } {
  const bytes = Buffer.from(value);
  if (bytes.length <= 16_384) return { value, truncated: false };
  let cut = 16_384;
  while (cut > 0 && (bytes[cut]! & 0xc0) === 0x80) cut--;
  return { value: bytes.subarray(0, cut).toString('utf8'), truncated: true };
}

/** Convert one ticket and its optional embedded contact. */
export function decodeTicket(wire: Wire, expectedId = ''): { ticket: Ticket; contact?: TicketContact } {
  const ticketId = id(wire.id);
  if (!ticketId) throw new Error('ticket ID is missing');
  if (expectedId && ticketId !== expectedId) throw new Error('ticket response is for another ticket');
  const status = text(wire.status);
  if (!status.trim()) throw new Error('ticket status is missing');
  const description = truncate(text(wire.description));
  const ticket: Wire = { id: ticketId, status, createdAt: timestamp('createdTime', wire.createdTime), modifiedAt: timestamp('modifiedTime', wire.modifiedTime) };
  for (const key of ['ticketNumber', 'subject', 'priority', 'channel', 'classification', 'email', 'webUrl']) optionalString(wire, key, ticket);
  for (const key of ['departmentId', 'contactId', 'assigneeId', 'teamId']) optionalId(wire, key, ticket);
  for (const key of ['isEscalated', 'isSpam']) optionalFlag(wire, key, ticket);
  optionalFlag(wire, 'isOverDue', ticket, 'isOverdue');
  for (const key of ['threadCount', 'commentCount']) optionalCount(wire, key, ticket);
  if (description.value) ticket.descriptionHtml = description.value;
  if (description.truncated) ticket.isDescriptionTruncated = true;
  const type = text(wire.statusType);
  if (type) ticket.statusType = ({ OPEN: 'Open', ONHOLD: 'On Hold', CLOSED: 'Closed' } as Record<string, string>)[type.toUpperCase().replaceAll(' ', '')] ?? type;
  if (wire.dueDate) ticket.dueAt = timestamp('dueDate', wire.dueDate);
  if (wire.closedTime) ticket.closedAt = timestamp('closedTime', wire.closedTime);
  let contact: TicketContact | undefined;
  if (wire.contact != null) {
    const embedded = object(wire.contact);
    const contactId = id(embedded.id);
    if (contactId) {
      const result: Wire = { id: contactId };
      for (const key of ['firstName', 'lastName', 'email']) optionalString(embedded, key, result);
      contact = result as unknown as TicketContact;
    }
  }
  return { ticket: ticket as unknown as Ticket, ...(contact ? { contact } : {}) };
}

/** Decode one thread summary. */
export function decodeThread(wire: Wire): TicketThread {
  const threadId = id(wire.id);
  if (!threadId) throw new Error('thread has no ID');
  const summary = truncate(text(wire.summary));
  const result: Wire = { id: threadId, createdAt: timestamp('createdTime', wire.createdTime) };
  for (const key of ['channel', 'direction', 'visibility', 'status']) optionalString(wire, key, result);
  if (summary.value) result.summary = summary.value;
  if (summary.truncated) result.isSummaryTruncated = true;
  optionalFlag(wire, 'isDescriptionThread', result);
  if (wire.author != null) { const author = object(wire.author); optionalString(author, 'name', result, 'authorName'); optionalString(author, 'type', result, 'authorType'); }
  return result as unknown as TicketThread;
}

/** Decode one public or private comment. */
export function decodeComment(wire: Wire): TicketComment {
  const commentId = id(wire.id);
  if (!commentId) throw new Error('comment has no ID');
  let content = text(wire.content), contentType = text(wire.contentType);
  if (wire.plainText !== undefined && wire.plainText !== null && contentType.toLowerCase() !== 'plaintext') { content = text(wire.plainText); contentType = 'plainText'; }
  const bounded = truncate(content);
  const result: Wire = { id: commentId, isPublic: flag(wire.isPublic), content: bounded.value, commentedAt: timestamp('commentedTime', wire.commentedTime) };
  if (contentType) result.contentType = contentType;
  if (bounded.truncated) result.isContentTruncated = true;
  optionalId(wire, 'commenterId', result);
  if (wire.commenter != null) { const commenter = object(wire.commenter); optionalString(commenter, 'name', result, 'commenterName'); optionalString(commenter, 'type', result, 'commenterType'); }
  if (wire.modifiedTime) result.modifiedAt = timestamp('modifiedTime', wire.modifiedTime);
  return result as unknown as TicketComment;
}

/** Read a bounded list response, allowing Zoho Desk's empty 204. */
export function decodeList(body: Uint8Array, status: number, limit: number): Wire[] {
  if (status === 204) return [];
  const page = decodeObject(body);
  if (!Array.isArray(page.data) || page.data.length > limit) throw new Error('list is not a JSON object with a data array');
  return page.data.map(object);
}
