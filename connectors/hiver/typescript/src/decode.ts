// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Conversation, ConversationDetails, Inbox, Note, SharedDraft, Tag } from './generated/models.js';

const idPattern = /^[A-Za-z0-9_-]{1,64}$/;
const statusPattern = /^[A-Za-z0-9 _-]{1,64}$/;
const gmailPattern = /^[A-Za-z0-9]{1,64}$/;
const assigneeTypePattern = /^[a-z_]{1,32}$/;

/** Validate an ID accepted by the Go connector. */
export function requireHiverID(field: string, value: string): void {
  if (typeof value !== 'string' || !idPattern.test(value)) throw new Error(`${field} must be a Hiver ID of 1 to 64 letters, digits, hyphens, or underscores`);
}
/** Escape one URL path segment with Go url.PathEscape's safe alphabet. */
export function pathEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%(24|26|2B|3D|3A|40)/gi, (_, hex: string) => String.fromCharCode(Number.parseInt(hex, 16))); }
/** Path for one shared inbox. */
export function inboxPath(inboxId: string): string { return `/inboxes/${pathEscape(inboxId)}`; }
/** Path for one conversation. */
export function conversationPath(inboxId: string, conversationId: string): string { return `${inboxPath(inboxId)}/conversations/${pathEscape(conversationId)}`; }
/** Decode one Hiver list envelope. */
export function decodeListPage(body: Uint8Array): { results: unknown[]; nextPageToken: string } {
  const root = object(parseProviderJSON(body));
  const data = object(root.data);
  if (!Array.isArray(data.results)) throw new Error('response is not a Hiver list page');
  let nextPageToken = '';
  if (data.pagination !== undefined && data.pagination !== null) {
    const pagination = object(data.pagination);
    if (pagination.next_page !== undefined && pagination.next_page !== null) nextPageToken = string(pagination.next_page);
  }
  if (nextPageToken && (Buffer.byteLength(nextPageToken) > 4096 || !/^[A-Za-z0-9+/=_.-]+$/.test(nextPageToken))) throw new Error('next_page is not a page token');
  return { results: data.results, nextPageToken };
}
/** Decode a Hiver single-object envelope. */
export function decodeSingleData(body: Uint8Array): Record<string, unknown> {
  const root = object(parseProviderJSON(body));
  const data = root.data;
  if (Array.isArray(data)) {
    if (data.length !== 1) throw new Error('data array does not hold exactly one object');
    return object(data[0]);
  }
  return object(data);
}
/** Decode an inbox from provider wire fields. */
export function decodeInbox(raw: unknown): Inbox {
  const wire = object(raw);
  const id = wireID(wire.id);
  requireHiverID('inbox id', id);
  return { id, displayName: optionalString(wire.display_name), email: optionalString(wire.email).trim(),
    ...(optionalString(wire.channel_type) ? { channelType: optionalString(wire.channel_type) } : {}), isAuthorized: optionalBoolean(wire.is_authorised) };
}
/** Decode a conversation and message IDs. */
export function decodeConversation(raw: unknown, inboxId: string): ConversationDetails {
  const wire = object(raw);
  const id = wireID(wire.id);
  requireHiverID('conversation id', id);
  const status = string(wire.status);
  if (!statusPattern.test(status)) throw new Error(`conversation ${id} status is missing or invalid`);
  const conversation: Conversation = { id, inboxId, status: status === 'close' ? 'closed' : status, tagIds: [] };
  if (wire.assignee !== undefined && wire.assignee !== null) {
    const assignee = object(wire.assignee);
    const assigneeId = wireID(assignee.assignee_id);
    if (assigneeId) {
      requireHiverID('assignee id', assigneeId);
      const type = string(assignee.assignee_type);
      if (!assigneeTypePattern.test(type)) throw new Error(`conversation ${id} assignee is invalid`);
      conversation.assignee = { id: assigneeId, type };
    }
  }
  const tagIds = optionalArray(wire.tag_ids).map(rawId => { const tagId = wireID(rawId); requireHiverID('tag id', tagId); return tagId; });
  conversation.tagIds = tagIds;
  const gmailThreadId = optionalString(wire.gmail_thread_id);
  if (gmailThreadId) { if (!gmailPattern.test(gmailThreadId)) throw new Error(`conversation ${id} gmail_thread_id is invalid`); conversation.gmailThreadId = gmailThreadId; }
  const permalink = optionalString(wire.private_permalink);
  if (permalink) {
    let parsed: URL;
    try { parsed = new URL(permalink); } catch { throw new Error('private_permalink is not an HTTPS URL'); }
    if (parsed.protocol !== 'https:' || parsed.username || parsed.password || Buffer.byteLength(permalink) > 2048) throw new Error('private_permalink is not an HTTPS URL');
    conversation.privatePermalink = permalink;
  }
  const messages = optionalArray(wire.message_ids).map(rawMessage => {
    const message = object(rawMessage);
    const hiverMessageId = wireID(message.hiver_message_id);
    const gmailMessageId = optionalString(message.gmail_message_id);
    if (hiverMessageId) requireHiverID('hiver_message_id', hiverMessageId);
    if (gmailMessageId && !gmailPattern.test(gmailMessageId) || !hiverMessageId && !gmailMessageId) throw new Error(`conversation ${id} has an invalid message ID`);
    return { ...(hiverMessageId ? { hiverMessageId } : {}), ...(gmailMessageId ? { gmailMessageId } : {}) };
  });
  return { conversation, messages };
}
/** Decode a tag from a shared inbox. */
export function decodeTag(raw: unknown): Tag {
  const wire = object(raw);
  const id = wireID(wire.id); requireHiverID('tag id', id);
  return { id, name: optionalString(wire.name) };
}
/** Decode a created internal note. */
export function decodeNote(body: Uint8Array): Note {
  const wire = decodeSingleData(body);
  const id = wireID(wire.id); requireHiverID('note id', id);
  const conversationId = wireID(wire.conversation_id);
  if (conversationId) requireHiverID('note conversation_id', conversationId);
  const author = wire.author === undefined || wire.author === null ? undefined : object(wire.author);
  const authorId = wireID(author?.id);
  if (authorId) requireHiverID('note author id', authorId);
  const authorEmail = optionalString(author?.email);
  const createdAt = optionalString(wire.created_at);
  const timestamp = createdAt ? parseRFC3339(createdAt) : '0001-01-01T00:00:00Z';
  return { id, ...(conversationId ? { conversationId } : {}), ...(authorId ? { authorId } : {}), ...(authorEmail ? { authorEmail } : {}), createdAt: timestamp };
}
/** Decode a created shared draft. */
export function decodeSharedDraft(body: Uint8Array): SharedDraft {
  const wire = decodeSingleData(body);
  const id = wireID(wire.shared_draft_id); requireHiverID('shared_draft_id', id);
  const conversationId = wireID(wire.id);
  const replyToHiverMessageId = wireID(wire.reply_to_message_id);
  if (conversationId) requireHiverID('conversation id', conversationId);
  if (replyToHiverMessageId) requireHiverID('message id', replyToHiverMessageId);
  return { id, ...(conversationId ? { conversationId } : {}), ...(replyToHiverMessageId ? { replyToHiverMessageId } : {}) };
}
/** Decode an object, rejecting provider shape errors. */
export function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('provider object is invalid'); return value as Record<string, unknown>; }
/** Decode a required string. */
export function string(value: unknown): string { if (typeof value !== 'string') throw new Error('provider string is invalid'); return value; }
/** Decode a nullable Go wire string. */
export function optionalString(value: unknown): string { return value === undefined || value === null ? '' : string(value); }
function optionalBoolean(value: unknown): boolean { if (value === undefined || value === null) return false; if (typeof value !== 'boolean') throw new Error('provider boolean is invalid'); return value; }
function optionalArray(value: unknown): unknown[] { if (value === undefined || value === null) return []; if (!Array.isArray(value)) throw new Error('provider array is invalid'); return value; }
/** Decode a Hiver ID represented by a string or nonnegative integer. */
export function wireID(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' && Number.isSafeInteger(value) && value >= 0) return String(value);
  throw new Error('ID is neither a string nor an integer');
}
function parseRFC3339(value: string): string {
  const match = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)([.,](\d+))?(Z|([+-])(\d\d):(\d\d))$/.exec(value);
  if (!match) throw new Error('note created_at is not an RFC 3339 time');
  const year = Number(match[1]); const month = Number(match[2]); const day = Number(match[3]);
  const hour = Number(match[4]); const minute = Number(match[5]); const second = Number(match[6]);
  const offsetHour = Number(match[11] ?? 0); const offsetMinute = Number(match[12] ?? 0);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const monthDays = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > monthDays[month - 1]! || hour > 23 || minute > 59 || second > 59 || offsetHour > 99 || offsetMinute > 59) throw new Error('note created_at is not an RFC 3339 time');
  const local = new Date(0);
  local.setUTCFullYear(year, month - 1, day);
  local.setUTCHours(hour, minute, second, 0);
  const direction = match[10] === '-' ? -1 : 1;
  const utc = new Date(local.getTime() - direction * (offsetHour * 60 + offsetMinute) * 60_000);
  if (!Number.isFinite(utc.getTime())) throw new Error('note created_at is not an RFC 3339 time');
  const fraction = (match[8] ?? '').slice(0, 9).replace(/0+$/, '');
  return utc.toISOString().replace(/\.000Z$/, fraction ? `.${fraction}Z` : 'Z');
}

function parseProviderJSON(body: Uint8Array): unknown {
  return JSON.parse(Buffer.from(body).toString('utf8'), (_key: string, value: unknown, context?: { source?: string }) =>
    typeof value === 'number' && Number.isInteger(value) ? context?.source ?? String(value) : value) as unknown;
}
