// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Parser } from 'htmlparser2';
import { MessageSchema, type Message, type MessageSender, type PostMessageOutput } from './generated/models.js';

const messageID = /^[0-9]{1,20}$/;
const teamID = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
const channelID = /^19:[A-Za-z0-9._-]{1,256}@thread\.[A-Za-z0-9]{1,32}$/;
const chatID = /^19:[A-Za-z0-9._-]{1,256}@[A-Za-z0-9.]{1,64}$/;
const goTimestamp = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):(\d{2}))$/;
const blockTags = new Set(['p', 'div', 'li', 'tr', 'blockquote', 'pre', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'table']);

/** Validate the team and channel ID forms accepted by Graph operations. */
export function validateTeamChannel(team: string, channel: string): void {
  if (!teamID.test(team)) throw new TypeError('teamId must be a team GUID such as fbe2bf47-16c8-47cf-b4a5-4b9b187c508b');
  if (!channelID.test(channel)) throw new TypeError('channelId must be a channel ID such as 19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2');
}
/** Validate a decimal Teams message ID. */
export function validateMessageID(field: string, value: string): void {
  if (!messageID.test(value)) throw new TypeError(`${field} must be a decimal Teams message ID such as 1616989510408`);
}
/** Validate a chat ID. */
export function validateChatID(value: string): void {
  if (!chatID.test(value)) throw new TypeError('chatId must be a chat ID such as 19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2');
}
/** Return the Graph channel messages path. */
export function channelMessagesPath(team: string, channel: string): string {
  return `/teams/${team}/channels/${channel}/messages`;
}
/** Return the Graph thread replies path. */
export function threadRepliesPath(team: string, channel: string, root: string): string {
  return `${channelMessagesPath(team, channel)}/${root}/replies`;
}
/** Escape one component exactly as Go url.PathEscape. */
export function pathEscape(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`)
    .replace(/%3A/gi, ':').replace(/%40/gi, '@').replace(/%24/gi, '$').replace(/%26/gi, '&')
    .replace(/%2B/gi, '+').replace(/%3D/gi, '=');
}
/** Encode query values exactly as Go url.Values.Encode. */
export function queryEncode(values: Record<string, string>): string {
  const encode = (value: string) => encodeURIComponent(value)
    .replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`)
    .replace(/%20/g, '+');
  return Object.keys(values).sort((a, b) => Buffer.compare(Buffer.from(a), Buffer.from(b)))
    .map(key => `${encode(key)}=${encode(values[key]!)}`).join('&');
}
/** Keep only printable provider correlation tokens. */
export function safeToken(value: unknown): string {
  return typeof value === 'string' && Buffer.byteLength(value) > 0 && Buffer.byteLength(value) <= 128 && /^[!-~]+$/.test(value) ? value : '';
}
/** Keep a valid HTTPS Teams link without reflecting provider controls. */
export function safeWebURL(value: unknown): string {
  if (typeof value !== 'string' || Buffer.byteLength(value) > 2048 || !value.startsWith('https://') || !/^[!-~]+$/.test(value)) return '';
  try { const url = new URL(value); return url.hostname && !url.username && !url.password ? value : ''; }
  catch { return ''; }
}
/** Parse Go RFC3339Nano time, retaining nine fractional digits and normalizing to UTC. */
export function parseGraphTime(value: unknown, field: string): string {
  if (typeof value !== 'string') throw new TypeError(`${field} is invalid`);
  const match = goTimestamp.exec(value);
  if (!match) throw new TypeError(`${field} is invalid`);
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, fraction, zone, sign, zoneHourText, zoneMinuteText] = match;
  const year = Number(yearText), month = Number(monthText), day = Number(dayText);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (!year || month < 1 || month > 12 || day < 1 || day > days[month - 1]! || Number(hourText) > 23 || Number(minuteText) > 59 || Number(secondText) > 59 || Number(zoneHourText ?? 0) > 24 || Number(zoneMinuteText ?? 0) > 60) throw new TypeError(`${field} is invalid`);
  const offset = zone === 'Z' ? 0 : (Number(zoneHourText) * 60 + Number(zoneMinuteText)) * (sign === '-' ? -1 : 1);
  const localDate = new Date(0);
  localDate.setUTCFullYear(year, month - 1, day);
  localDate.setUTCHours(Number(hourText), Number(minuteText), Number(secondText), 0);
  const instant = new Date(localDate.getTime() - offset * 60_000);
  if (!Number.isFinite(instant.getTime())) throw new TypeError(`${field} is invalid`);
  const digits = (fraction ?? '').slice(0, 9).replace(/0+$/, '');
  return instant.toISOString().slice(0, 19) + (digits ? `.${digits}` : '') + 'Z';
}
/** Extract visible Teams text, preserving line breaks from block tags. */
export function extractMessageText(contentType: string, content: string): string {
  if (contentType !== 'html') return content.replaceAll('\r\n', '\n').trim();
  let pieces = '', skipped = 0, atLineStart = true;
  const boundary = () => { if (pieces && !atLineStart) { pieces += '\n'; atLineStart = true; } };
  const parser = new Parser({
    onopentag(name, attributes) {
      if (name === 'script' || name === 'style') { skipped++; return; }
      if (skipped) return;
      if (name === 'br') { pieces += '\n'; atLineStart = true; }
      else if (name === 'img' || name === 'emoji') { const text = attributes.alt ?? ''; pieces += text; if (text.trim()) atLineStart = false; }
      else if (blockTags.has(name)) boundary();
    },
    ontext(text) { if (!skipped) { const replaced = text.replace(/[\r\n\t]/g, ' '); pieces += replaced; if (replaced.trim()) atLineStart = false; } },
    onclosetag(name) { if (name === 'script' || name === 'style') skipped = Math.max(0, skipped - 1); else if (!skipped && blockTags.has(name)) boundary(); },
  }, { decodeEntities: true });
  parser.write(content); parser.end();
  const lines: string[] = []; let previousBlank = true;
  for (const line of pieces.split('\n')) {
    const folded = line.trim().split(/\s+/u).filter(Boolean).join(' ');
    if (folded) { lines.push(folded); previousBlank = false; }
    else if (!previousBlank) { lines.push(''); previousBlank = true; }
  }
  return lines.join('\n').trim();
}
/** Reduce text to whitespace-separated words for read-back attribution. */
export function comparisonText(value: string): string { return value.trim().split(/\s+/u).filter(Boolean).join(' '); }
/** Validate content and construct the exact Graph chatMessage body. */
export function buildContent(content: string, contentType: string | undefined, importance: string | undefined, subject: string, maximumBytes: number): { body: Record<string, unknown>; comparison: string } {
  const type = contentType || 'text';
  if (type !== 'text' && type !== 'html') throw new TypeError('contentType must be text or html');
  if (importance && !['normal', 'high', 'urgent'].includes(importance)) throw new TypeError('importance must be normal, high, or urgent');
  if (!content.trim()) throw new TypeError('content is required');
  if (/[\r\n]/.test(subject)) throw new TypeError('subject must be one line');
  if ([...subject].length > 1024) throw new TypeError('subject must be at most 1024 characters');
  if (Buffer.byteLength(content) + Buffer.byteLength(subject) > maximumBytes) throw new TypeError(`content and subject exceed the connection's maxMessageBytes limit of ${maximumBytes} bytes`);
  const text = extractMessageText(type, content);
  if (!text.trim()) throw new TypeError('content has no visible text');
  const body: Record<string, unknown> = { body: { contentType: type, content } };
  if (subject) body.subject = subject;
  if (importance) body.importance = importance;
  return { body, comparison: comparisonText(text) };
}
/** Decode one Graph message without keeping unsafe provider fields. */
export function decodeMessage(value: unknown, maximumCharacters = 0): Message {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('message ID is invalid');
  const resource = value as Record<string, unknown>;
  if (typeof resource.id !== 'string' || !messageID.test(resource.id)) throw new TypeError('message ID is invalid');
  let createdAt: string;
  try { createdAt = parseGraphTime(resource.createdDateTime, 'createdDateTime'); }
  catch { throw new TypeError('message createdDateTime is invalid'); }
  let lastEditedAt: string | undefined;
  if (typeof resource.lastEditedDateTime === 'string') {
    try { lastEditedAt = parseGraphTime(resource.lastEditedDateTime, 'lastEditedDateTime'); }
    catch { /* Go ignores an invalid optional edit time. */ }
  }
  const from = resource.from && typeof resource.from === 'object' ? resource.from as Record<string, unknown> : undefined;
  const user = from?.user && typeof from.user === 'object' ? from.user as Record<string, unknown> : undefined;
  const application = from?.application && typeof from.application === 'object' ? from.application as Record<string, unknown> : undefined;
  const identity = safeToken(user?.id) ? user : safeToken(application?.id) ? application : undefined;
  const sender: MessageSender = {};
  if (identity === user) sender.userId = safeToken(identity?.id);
  else if (identity) sender.applicationId = safeToken(identity.id);
  if (identity && typeof identity.displayName === 'string') sender.displayName = [...identity.displayName].filter(char => !/[\p{Cc}\p{Cf}\uFFFD]/u.test(char)).slice(0, 256).join('');
  const deleted = resource.deletedDateTime !== null && resource.deletedDateTime !== undefined;
  const body = resource.body && typeof resource.body === 'object' ? resource.body as Record<string, unknown> : undefined;
  const contentType = safeToken(body?.contentType);
  const text = body && !deleted ? extractMessageText(contentType, typeof body.content === 'string' ? body.content : '') : '';
  const codepoints = [...text];
  const output: Message = {
    id: resource.id, messageType: safeToken(resource.messageType).slice(0, 64), createdAt, sender,
    text: maximumCharacters > 0 && codepoints.length > maximumCharacters ? codepoints.slice(0, maximumCharacters).join('') : text,
  };
  if (typeof resource.replyToId === 'string' && messageID.test(resource.replyToId)) output.replyToId = resource.replyToId;
  if (lastEditedAt) output.lastEditedAt = lastEditedAt;
  if (deleted) output.isDeleted = true;
  if (typeof resource.subject === 'string') output.subject = [...resource.subject].filter(char => !/[\p{Cc}\p{Cf}\uFFFD]/u.test(char)).slice(0, 1024).join('');
  const importance = safeToken(resource.importance).slice(0, 64);
  if (importance) output.importance = importance;
  if (body && !deleted && contentType) output.contentType = contentType.slice(0, 64);
  if (maximumCharacters > 0 && codepoints.length > maximumCharacters) output.isTextTruncated = true;
  const webURL = safeWebURL(resource.webUrl);
  if (webURL) output.webUrl = webURL;
  return MessageSchema.parse(output);
}
/** Fill the requested IDs after Graph confirms a post. */
export function postedOutput(requested: PostMessageOutput, message: Message, readBack: boolean): PostMessageOutput {
  return { ...requested, messageId: message.id, createdAt: message.createdAt, sender: message.sender,
    ...(message.webUrl ? { webUrl: message.webUrl } : {}), ...(readBack ? { isConfirmedByReadBack: true } : {}) };
}
