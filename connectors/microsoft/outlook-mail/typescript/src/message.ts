// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Attachment, EmailAddress, Message, MessageSummary } from './generated/models.js';
import { Parser } from 'htmlparser2';
export const summaryProperties = ['id','conversationId','internetMessageId','parentFolderId','subject','from','sender','replyTo','toRecipients','ccRecipients','receivedDateTime','sentDateTime','isRead','isDraft','flag','categories','importance','hasAttachments','bodyPreview'];
const wellKnownFolders = new Set(['archive','clutter','conflicts','conversationhistory','deleteditems','drafts','inbox','junkemail','localfailures','msgfolderroot','outbox','recoverableitemsdeletions','scheduled','searchfolders','sentitems','serverfailures','syncissues']);
const zeroTime = '0001-01-01T00:00:00Z';
export interface GraphMessageWire { id: string; conversationId?: string; internetMessageId?: string; parentFolderId?: string; subject?: string; from?: GraphRecipient; sender?: GraphRecipient; replyTo?: GraphRecipient[]; toRecipients?: GraphRecipient[]; ccRecipients?: GraphRecipient[]; bccRecipients?: GraphRecipient[]; receivedDateTime?: string; sentDateTime?: string; createdDateTime?: string; isRead?: boolean; isDraft?: boolean; flag?: {flagStatus?: string}; categories?: string[]; importance?: string; hasAttachments?: boolean; bodyPreview?: string; webLink?: string; body?: {contentType?: string; content?: string} }
interface GraphRecipient { emailAddress?: { name?: string; address?: string } }
function object(value: unknown): Record<string,unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('response is not a message object'); return value as Record<string,unknown>; }
export function parseJSON(body: Uint8Array): unknown {
  const parse = JSON.parse as (text: string, reviver: (key: string, value: unknown, context?: { source?: string }) => unknown) => unknown;
  return parse(Buffer.from(body).toString('utf8'), (key, value, context) => {
    if (key === 'size' && typeof value === 'number' && context?.source && !/^-?(?:0|[1-9][0-9]*)$/.test(context.source))
      throw new TypeError('attachment size must be an integer JSON literal');
    return value;
  });
}
export function validateGraphID(value: string, field = 'messageId'): void { if (!value || Buffer.byteLength(value) > 1024 || !/^[A-Za-z0-9=+_-]+$/.test(value)) throw new TypeError(`${field} must be a Microsoft Graph ID from an earlier Result, such as the id of searchMessages`); }
export function resolveFolder(folder: string, fallback = '', field = 'folder'): string { const value = folder.trim() || fallback; if (!value) throw new TypeError(`${field} is required: a folder ID from the mail folder picker or a well-known name such as archive`); if (wellKnownFolders.has(value.toLowerCase())) return value.toLowerCase(); try { validateGraphID(value, field); } catch { throw new TypeError(`${field} must be a folder ID from the mail folder picker or a well-known name such as inbox, archive, or deleteditems`); } return value; }
export function validateAddress(value: string, field: string): void { if (!/^[^\s<>@,;()]+@[^\s<>@,;()]+$/.test(value) || value.includes('..')) throw new TypeError(`${field} must contain bare addresses such as jane@acme.example.com`); }
export function validateSingleLine(value: string, field: string, maximum: number): void { if ([...value].length > maximum) throw new TypeError(`${field} can be at most ${maximum} characters`); if ([...value].some(character => { const code=character.codePointAt(0)!; return code < 32 || code === 127; })) throw new TypeError(`${field} must be one line without control characters`); }
export function parseGraphMessage(value: unknown): GraphMessageWire {
  const record = object(value);
  for (const name of ['id','conversationId','internetMessageId','parentFolderId','subject','importance','bodyPreview','webLink']) {
    if (record[name] != null && typeof record[name] !== 'string') throw new TypeError('response is not a message object');
  }
  for (const name of ['isRead','isDraft','hasAttachments']) {
    if (record[name] != null && typeof record[name] !== 'boolean') throw new TypeError('response is not a message object');
  }
  for (const name of ['receivedDateTime','sentDateTime','createdDateTime']) {
    if (record[name] != null) {
      if (typeof record[name] !== 'string') throw new TypeError('response is not a message object');
      graphTimestampUTC(record[name]);
    }
  }
  if (record.categories != null && (!Array.isArray(record.categories) || record.categories.some(item => typeof item !== 'string' && item !== null))) throw new TypeError('response is not a message object');
  if (record.flag != null) {
    const flag = object(record.flag);
    if (flag.flagStatus != null && typeof flag.flagStatus !== 'string') throw new TypeError('response is not a message object');
  }
  for (const name of ['from','sender','replyTo','toRecipients','ccRecipients','bccRecipients']) {
    if (record[name] == null) continue;
    const values = ['from','sender'].includes(name) ? [record[name]] : record[name];
    if (!Array.isArray(values)) throw new TypeError('response is not a message object');
    for (const value of values) {
      if (value == null) continue;
      const recipient = object(value);
      if (recipient.emailAddress == null) continue;
      const address = object(recipient.emailAddress);
      for (const field of ['name','address']) if (address[field] != null && typeof address[field] !== 'string') throw new TypeError('response is not a message object');
    }
  }
  if (record.body != null) {
    const body = object(record.body);
    for (const name of ['contentType','content']) if (body[name] != null && typeof body[name] !== 'string') throw new TypeError('response is not a message object');
  }
  if (typeof record.id !== 'string') throw new TypeError('response has no usable message id');
  try { validateGraphID(record.id, 'message id'); } catch { throw new TypeError('response has no usable message id'); }
  return record as unknown as GraphMessageWire;
}
function toAddress(value: GraphRecipient | undefined): EmailAddress | undefined { const address = value?.emailAddress?.address; return address ? { ...(value?.emailAddress?.name ? {name:value.emailAddress.name}:{}), address } : undefined; }
function toAddresses(values: GraphRecipient[] | undefined): EmailAddress[] { return (values ?? []).slice(0,50).map(toAddress).filter((value): value is EmailAddress => value !== undefined); }
/** Validate a Graph timestamp with Go's RFC 3339 rules and retain nanoseconds in UTC. */
export function graphTimestampUTC(value: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))$/.exec(value);
  if (!match) throw new TypeError('timestamp is not an RFC 3339 time');
  const year = Number(match[1]), month = Number(match[2]), day = Number(match[3]), hour = Number(match[4]), minute = Number(match[5]), second = Number(match[6]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]! || hour > 23 || minute > 59 || second > 59 || Number(match[10] ?? 0) > 24 || Number(match[11] ?? 0) > 60) throw new TypeError('timestamp is not an RFC 3339 time');
  const nanos = (match[7] ?? '').slice(0, 9).padEnd(9, '0');
  const local = new Date(0); local.setUTCFullYear(year, month - 1, day); local.setUTCHours(hour, minute, second, Number(nanos.slice(0, 3)));
  const offset = match[8] === 'Z' ? 0 : (Number(match[10]) * 60 + Number(match[11])) * (match[9] === '+' ? 1 : -1);
  const iso = new Date(local.getTime() - offset * 60_000).toISOString();
  const fraction = nanos.replace(/0+$/, '');
  return iso.slice(0, 19) + (fraction ? '.' + fraction : '') + 'Z';
}
function timeUTC(value: string | undefined): string { return value ? graphTimestampUTC(value) : zeroTime; }
export function summarizeMessage(message: GraphMessageWire): MessageSummary { return {
  id: message.id, ...(message.conversationId ? {conversationId:message.conversationId}:{}), ...(message.internetMessageId ? {internetMessageId:message.internetMessageId}:{}), ...(message.parentFolderId ? {folderId:message.parentFolderId}:{}),
  subject:[...(message.subject ?? '')].slice(0,255).join(''), ...(toAddress(message.from) ? {from:toAddress(message.from)}:{}), ...(toAddress(message.sender) ? {sender:toAddress(message.sender)}:{}),
  ...(toAddresses(message.replyTo).length ? {replyTo:toAddresses(message.replyTo)}:{}), ...(toAddresses(message.toRecipients).length ? {to:toAddresses(message.toRecipients)}:{}), ...(toAddresses(message.ccRecipients).length ? {cc:toAddresses(message.ccRecipients)}:{}),
  receivedAt:timeUTC(message.receivedDateTime), ...(message.sentDateTime && !message.isDraft ? {sentAt:timeUTC(message.sentDateTime)}:{}), isRead:message.isRead ?? false, ...(message.isDraft ? {isDraft:true}:{}),
  flagStatus:message.flag?.flagStatus || 'notFlagged', ...(message.categories?.length ? {categories:message.categories.slice(0,25)}:{}), ...(message.importance ? {importance:message.importance}:{}), hasAttachments:message.hasAttachments ?? false,
  ...(message.bodyPreview ? {preview:[...message.bodyPreview].slice(0,255).join('')}:{})
}; }
export function recipientAddresses(message: GraphMessageWire): string[] { return [...message.toRecipients ?? [],...message.ccRecipients ?? [],...message.bccRecipients ?? []].map(value => value?.emailAddress?.address ?? '').filter(Boolean); }
export function buildGraphRecipients(addresses: string[]): GraphRecipient[] { return addresses.map(address => ({emailAddress:{address,name:''}})); }
function extractHTMLText(document: string): string {
  const text: string[] = [];
  let skippedDepth = 0;
  const parser = new Parser({
    onopentag(name) {
      if (['script', 'style', 'head'].includes(name)) skippedDepth++;
      else if (['br', 'p', 'div', 'li', 'tr', 'h1', 'h2', 'h3', 'h4', 'blockquote'].includes(name)) text.push('\n');
    },
    onclosetag(name) {
      if (['script', 'style', 'head'].includes(name)) skippedDepth = Math.max(0, skippedDepth - 1);
    },
    ontext(value) { if (skippedDepth === 0) text.push(value); },
  }, { decodeEntities: true });
  parser.end(document);
  return text.join('').trim();
}
/** Decode one full Graph message, including HTML text and bounded attachments. */
export function decodeFullMessage(message: GraphMessageWire): Message {
  const summary = summarizeMessage(message);
  const body = message.body?.content ?? '';
  const isHTML = message.body?.contentType?.toLowerCase() === 'html';
  const text = isHTML ? extractHTMLText(body) : body;
  const bytes = Buffer.from(text);
  let cut = Math.min(bytes.length, 65536);
  while (cut > 0 && cut < bytes.length && (bytes[cut]! & 0xc0) === 0x80) cut--;
  return { ...summary, ...(toAddresses(message.bccRecipients).length ? { bcc: toAddresses(message.bccRecipients) } : {}),
    text: bytes.subarray(0, cut).toString('utf8'), ...(cut < bytes.length ? { isTextTruncated: true } : {}),
    textSource: isHTML ? 'html' : 'text', ...(message.webLink ? { webLink: message.webLink } : {}) };
}
export function decodeAttachments(value: unknown): {attachments: Attachment[]; truncated: boolean} { const envelope=object(value); if(!Array.isArray(envelope.value)) throw new TypeError('response has no value list'); const attachments: Attachment[]=[]; for(const item of envelope.value){ if(attachments.length===50) return {attachments,truncated:true}; const record=object(item); if(typeof record.id!=='string'||!record.id||record.size != null && (typeof record.size!=='number'||!Number.isInteger(record.size)||record.size<0)) throw new TypeError('an attachment has no id or a negative size'); const kind=record['@odata.type']==='#microsoft.graph.itemAttachment'?'item':record['@odata.type']==='#microsoft.graph.referenceAttachment'?'reference':'file'; attachments.push({id:record.id,name:typeof record.name==='string'?record.name:'',...(typeof record.contentType==='string'&&record.contentType?{contentType:record.contentType}:{}),sizeBytes:record.size as number ?? 0,...(record.isInline?{isInline:true}:{}),kind}); } return {attachments,truncated:typeof envelope['@odata.nextLink']==='string'&&!!envelope['@odata.nextLink']}; }
