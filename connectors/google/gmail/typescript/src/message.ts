// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { z } from 'zod';
import type { Message, ReplyToMessageInput, SendMessageInput } from './models.js';

const GmailMessagePartSchema: z.ZodType<GmailMessagePart> = z.lazy(() => z.object({
  mimeType: z.string().nullish(), headers: z.array(z.object({ name: z.string(), value: z.string() })).nullish(),
  body: z.object({ data: z.string().nullish() }).nullish(), parts: z.array(GmailMessagePartSchema).nullish(),
}));

/** Provider message part with nested MIME children. */
export interface GmailMessagePart {
  /** MIME content type. */
  mimeType?: string | null;
  /** Message headers supplied on the root part. */
  headers?: { name: string; value: string }[] | null;
  /** Base64url-encoded body. */
  body?: { data?: string | null } | null;
  /** Child parts of a multipart message. */
  parts?: GmailMessagePart[] | null;
}

/** Gmail message resource required by the reader. */
export const GmailMessageResourceSchema = z.object({
  id: z.string().nullish().transform(value => value ?? ''), threadId: z.string().nullish().transform(value => value ?? ''),
  labelIds: z.array(z.string()).nullish(), snippet: z.string().nullish(),
  internalDate: z.string().nullish(), payload: GmailMessagePartSchema.nullish(),
});
/** Gmail message resource required by the reader. */
export type GmailMessageResource = z.infer<typeof GmailMessageResourceSchema>;

/** Decode a Gmail full or metadata resource with the Go connector's message fields. */
export function decodeGmailMessage(resource: GmailMessageResource): Message {
  if (!resource.id || !resource.threadId || !resource.internalDate || !/^\d+$/.test(resource.internalDate)) throw new TypeError('Gmail message identity or date is missing');
  const receivedAt = new Date(Number(resource.internalDate));
  if (!Number.isFinite(receivedAt.getTime())) throw new TypeError('Gmail message date is invalid');
  const headers = new Map((resource.payload?.headers ?? []).map(header => [header.name.toLowerCase(), header.value]));
  const textBodies: string[] = [];
  const htmlBodies: string[] = [];
  visitGmailBody(resource.payload ?? {}, textBodies, htmlBodies);
  const to = splitAddressHeader(headers.get('to') ?? '');
  return {
    messageId: resource.id, threadId: resource.threadId, rfcMessageId: headers.get('message-id') ?? '',
    inReplyTo: headers.get('in-reply-to') ?? '', references: headers.get('references') ?? '',
    from: headers.get('from') ?? '', replyTo: headers.get('reply-to') ?? '', to,
    subject: decodeRFC2047Header(headers.get('subject') ?? ''), textBody: textBodies.join('\n'),
    htmlBody: htmlBodies.join('\n'), snippet: resource.snippet ?? '',
    receivedAt: receivedAt.toISOString().replace('.000Z', 'Z'), labelIds: resource.labelIds ?? [],
  };
}

function visitGmailBody(part: GmailMessagePart, textBodies: string[], htmlBodies: string[]): void {
  if (part.body?.data) {
    const encoded = part.body.data;
    if (!/^[A-Za-z0-9_-]*$/.test(encoded) || encoded.length % 4 === 1) throw new TypeError('Gmail body is not raw base64url');
    const decoded = Buffer.from(encoded, 'base64url').toString('utf8');
    if (part.mimeType?.toLowerCase() === 'text/plain') textBodies.push(decoded);
    if (part.mimeType?.toLowerCase() === 'text/html') htmlBodies.push(decoded);
  }
  for (const child of part.parts ?? []) visitGmailBody(child, textBodies, htmlBodies);
}

function decodeRFC2047Header(value: string): string {
  return value.replace(/=\?UTF-8\?([BQ])\?([^?]+)\?=/gi, (encoded, encoding: string, body: string) => {
    try {
      const bytes = encoding.toUpperCase() === 'B'
        ? Buffer.from(body, 'base64')
        : Buffer.from(body.replaceAll('_', ' ').replace(/=([0-9A-Fa-f]{2})/g, (_match, hex: string) => String.fromCharCode(parseInt(hex, 16))), 'binary');
      return bytes.toString('utf8');
    } catch { return encoded; }
  });
}

function splitAddressHeader(value: string): string[] {
  if (!value || /[\r\n]/.test(value)) return [];
  const addresses = value.split(/,\s*(?=(?:[^"]*"[^"]*")*[^"]*$)/).map(address => address.trim());
  const canonical: string[] = [];
  for (const address of addresses) {
    const parsed = parseMailbox(address);
    if (!parsed) return [];
    canonical.push(formatMailbox(parsed));
  }
  return canonical;
}

/** Parse one safe mailbox, rejecting ambiguous display-name separators. */
export function parseMailbox(value: string): { display: string; address: string } | undefined {
  if (Array.from(value).some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)) return undefined;
  const trimmed = value.trim();
  const angle = /^(.*?)<([^<>]+)>$/.exec(trimmed);
  if (!angle && /[<>]/.test(trimmed)) return undefined;
  const address = angle?.[2]?.trim() ?? trimmed;
  if (!/^[^<>\s,;@]+@[^<>\s,;@]+$/.test(address)) return undefined;
  let display = angle?.[1]?.trim() ?? '';
  if (display.startsWith('"')) {
    if (!/^"(?:[^"\\]|\\.)*"$/.test(display)) return undefined;
    display = display.slice(1, -1).replace(/\\(.)/g, '$1');
  } else if (/[,:;<>@"\\()[\]]/.test(display)) return undefined;
  return { display, address };
}

function formatMailbox(mailbox: { display: string; address: string }): string {
  if (!mailbox.display) return `<${mailbox.address}>`;
  const display = /[^\x20-\x7E]/.test(mailbox.display)
    ? encodeSubject(mailbox.display).replace('=?UTF-8?Q?', '=?utf-8?q?')
    : `"${mailbox.display.replaceAll('\\', '\\\\').replaceAll('"', '\\"')}"`;
  return `${display} <${mailbox.address}>`;
}

/** Return the safe reply target, preferring Reply-To. */
export function replyRecipient(message: Message): string {
  const recipient = message.replyTo || message.from;
  const parsed = recipient ? parseMailbox(recipient) : undefined;
  if (!parsed) throw new TypeError('Gmail source sender is invalid');
  return formatMailbox(parsed);
}

/** Validate sender, recipients, subject, and body before sending. */
export function validateSendInput(input: SendMessageInput, sender: string): string[] {
  if (!sender.trim() || !input.subject.trim() || /[\r\n]/.test(input.subject) || !input.textBody.trim() || input.to.length === 0) {
    throw new TypeError('sender, recipients, subject, and text body are required');
  }
  const senderAddress = parseMailbox(sender);
  if (!senderAddress || senderAddress.address.toLowerCase() !== sender.trim().toLowerCase()) throw new TypeError('authorized primary email is invalid');
  const seen = new Set<string>();
  const recipients: string[] = [];
  for (const recipient of input.to) {
    const parsed = parseMailbox(recipient);
    if (!parsed) throw new TypeError('recipient email is invalid');
    const canonical = parsed.address.toLowerCase();
    if (seen.has(canonical)) throw new TypeError('recipient email is duplicated');
    seen.add(canonical);
    recipients.push(formatMailbox(parsed));
  }
  return recipients;
}

/** Deterministic provider search identity for one Dex run and connector call. */
export function rfcMessageIDAddress(idempotencyKey: string, runId: string, flowStartedAt: Date): string {
  const runIdentity = `${runId}\0${Math.floor(flowStartedAt.getTime() / 1000)}`;
  return `${createHash('sha256').update(`${idempotencyKey}\0${runIdentity}`).digest('hex')}@dex.superdurable.dev`;
}

/** Build the exact raw RFC 5322 request body shared by sends and replies. */
export function buildMIMEMessage(callId: string, idempotencyKey: string, messageIdAddress: string, sender: string, recipients: string[], subject: string, textBody: string, htmlBody = '', replyHeaders?: { inReplyTo: string; references: string }): Uint8Array {
  if (/[\r\n]/.test(sender + subject + messageIdAddress + recipients.join('') + (replyHeaders?.inReplyTo ?? '') + (replyHeaders?.references ?? ''))) throw new TypeError('MIME header contains a newline');
  const headers = [`From: ${sender}`, `To: ${recipients.join(', ')}`, `Subject: ${encodeSubject(subject)}`, `Message-ID: <${messageIdAddress}>`];
  if (replyHeaders) headers.push(`In-Reply-To: ${replyHeaders.inReplyTo}`, `References: ${replyHeaders.references}`);
  headers.push(`X-Dex-Call-ID: ${callId}`, 'MIME-Version: 1.0');
  if (!htmlBody) {
    headers.push('Content-Type: text/plain; charset="UTF-8"', 'Content-Transfer-Encoding: 8bit');
    return Buffer.from(`${headers.join('\r\n')}\r\n\r\n${toCRLF(textBody)}`, 'utf8');
  }
  const boundary = `dex-${createHash('sha256').update(idempotencyKey).digest('hex').slice(0, 24)}`;
  headers.push(`Content-Type: multipart/alternative; boundary="${boundary}"`);
  const body = [
    `--${boundary}\r\nContent-Type: text/plain; charset="UTF-8"\r\n\r\n${toCRLF(textBody)}\r\n`,
    `--${boundary}\r\nContent-Type: text/html; charset="UTF-8"\r\n\r\n${toCRLF(htmlBody)}\r\n`,
    `--${boundary}--\r\n`,
  ].join('');
  return Buffer.from(`${headers.join('\r\n')}\r\n\r\n${body}`, 'utf8');
}

/** Build a reply's subject and threading fields from its source message. */
export function buildReplyMIMEMessage(callId: string, idempotencyKey: string, messageIdAddress: string, sender: string, source: Message, input: ReplyToMessageInput): Uint8Array {
  const recipient = replyRecipient(source);
  const subject = /^re:/i.test(source.subject.trim()) ? source.subject : `Re: ${source.subject}`;
  return buildMIMEMessage(callId, idempotencyKey, messageIdAddress, sender, [recipient], subject, input.textBody, input.htmlBody, {
    inReplyTo: source.rfcMessageId ?? '', references: `${source.references ?? ''} ${source.rfcMessageId ?? ''}`.trim(),
  });
}

function toCRLF(value: string): string { return value.replace(/\r\n|\r/g, '\n').replaceAll('\n', '\r\n'); }

function encodeSubject(value: string): string {
  if (/^[\x20-\x7E]*$/.test(value)) return value;
  const bytes = Buffer.from(value, 'utf8');
  const encoded = Array.from(bytes, byte => {
    if (byte === 32) return '_';
    if (byte >= 33 && byte <= 126 && byte !== 61 && byte !== 63 && byte !== 95) return String.fromCharCode(byte);
    return `=${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }).join('');
  return `=?UTF-8?Q?${encoded}?=`;
}
