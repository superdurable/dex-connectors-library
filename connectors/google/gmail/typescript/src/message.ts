// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { z } from 'zod';
import { DexMessageIDHeader } from './dex_message_id_header.js';
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
    messageId: resource.id, threadId: resource.threadId,
    ...(headers.get('message-id') ? { rfcMessageId: headers.get('message-id') } : {}),
    ...(headers.get('in-reply-to') ? { inReplyTo: headers.get('in-reply-to') } : {}),
    ...(headers.get('references') ? { references: headers.get('references') } : {}),
    from: headers.get('from') ?? '',
    ...(headers.get('reply-to') ? { replyTo: headers.get('reply-to') } : {}),
    ...(to.length ? { to } : {}),
    subject: decodeRFC2047Header(headers.get('subject') ?? ''),
    ...(textBodies.length ? { textBody: textBodies.join('\n') } : {}),
    ...(htmlBodies.length ? { htmlBody: htmlBodies.join('\n') } : {}),
    ...(resource.snippet ? { snippet: resource.snippet } : {}),
    // Go's time.Time JSON (RFC 3339 with trailing fractional zeros removed), such as .12Z for 120 ms.
    receivedAt: receivedAt.toISOString().replace(/\.(\d*?)0*Z$/, (_match, digits: string) => (digits ? `.${digits}Z` : 'Z')),
    ...(resource.labelIds?.length ? { labelIds: resource.labelIds } : {}),
  };
}

function visitGmailBody(part: GmailMessagePart, textBodies: string[], htmlBodies: string[]): void {
  if (part.body?.data) {
    const decoded = decodeGmailBodyData(part.body.data).toString('utf8');
    if (part.mimeType?.toLowerCase() === 'text/plain') textBodies.push(decoded);
    if (part.mimeType?.toLowerCase() === 'text/html') htmlBodies.push(decoded);
  }
  for (const child of part.parts ?? []) visitGmailBody(child, textBodies, htmlBodies);
}

// Gmail pads body data whenever its decoded length is not a multiple of three.
function decodeGmailBodyData(encoded: string): Buffer {
  const isUnpadded = /^[A-Za-z0-9_-]*$/.test(encoded) && encoded.length % 4 !== 1;
  const isPadded = /^[A-Za-z0-9_-]+={1,2}$/.test(encoded) && encoded.length % 4 === 0;
  if (!isUnpadded && !isPadded) throw new TypeError('Gmail body is not base64url');
  return Buffer.from(encoded, 'base64url');
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
  const canonical: string[] = [];
  for (const address of splitAddressList(value)) {
    const parsed = parseMailbox(address);
    if (!parsed) return [];
    canonical.push(formatMailbox(parsed));
  }
  return canonical;
}

// A comma ends a mailbox unless it is inside a quoted string, where a backslash escapes the next character.
function splitAddressList(value: string): string[] {
  const addresses: string[] = [];
  let isQuoted = false;
  let start = 0;
  for (let index = 0; index < value.length; index++) {
    const character = value[index];
    if (isQuoted && character === '\\') index++;
    else if (character === '"') isQuoted = !isQuoted;
    else if (character === ',' && !isQuoted) {
      addresses.push(value.slice(start, index).trim());
      start = index + 1;
    }
  }
  addresses.push(value.slice(start).trim());
  return addresses;
}

/**
 * Parse one safe mailbox, rejecting ambiguous display-name separators.
 *
 * A quoted display name is unescaped. An unquoted one is decoded as Go's
 * `net/mail` does: RFC 2047 encoded words are decoded, adjacent encoded words
 * are joined without the space between them, and other words are joined by one
 * space. Valid `utf-8`, `iso-8859-1`, and `us-ascii` words decode as in Go,
 * but invalid bytes in a `utf-8` word become U+FFFD, where Go keeps the raw
 * bytes. Another charset decodes with `TextDecoder`, and a word that cannot be
 * decoded stays literal. A display name made only of empty encoded words reads
 * as no name, as in Go 1.24; Go 1.26 and later reject that mailbox.
 */
export function parseMailbox(value: string): { display: string; address: string } | undefined {
  if (Array.from(value).some(character => (character < ' ' && character !== '\t') || character === '\x7F')) return undefined;
  const trimmed = value.trim();
  const angle = /^(.*?)<([^<>]+)>$/.exec(trimmed);
  if (!angle && /[<>]/.test(trimmed)) return undefined;
  const address = angle?.[2]?.trim() ?? trimmed;
  if (!/^[^<>\s,;@]+@[^<>\s,;@]+$/.test(address)) return undefined;
  const display = angle?.[1]?.trim() ?? '';
  if (display.startsWith('"')) {
    if (!/^"(?:[^"\\]|\\.)*"$/.test(display)) return undefined;
    return { display: display.slice(1, -1).replace(/\\(.)/g, '$1'), address };
  }
  if (/[,:;<>@"\\()[\]]/.test(display)) return undefined;
  return { display: decodeDisplayNamePhrase(display), address };
}

function decodeDisplayNamePhrase(phrase: string): string {
  const words: string[] = [];
  let encodedRun = '';
  for (const word of phrase.split(/[ \t]+/)) {
    if (!word) continue;
    const decoded = decodeEncodedWord(word);
    if (decoded !== undefined) {
      encodedRun += decoded;
      continue;
    }
    if (encodedRun) words.push(encodedRun);
    encodedRun = '';
    words.push(word);
  }
  if (encodedRun) words.push(encodedRun);
  return words.join(' ');
}

const EncodedWordPattern = /^=\?([^?]+)\?([BbQq])\?([^?]*)\?=$/;
const PaddedBase64Pattern = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;

// Go's mime.WordDecoder rules; undefined leaves the word literal.
function decodeEncodedWord(word: string): string | undefined {
  const match = EncodedWordPattern.exec(word);
  if (!match) return undefined;
  const [, charset = '', encoding = '', text = ''] = match;
  const bytes = encoding.toUpperCase() === 'B' ? decodeBEncodedText(text) : decodeQEncodedText(text);
  return bytes && decodeCharsetBytes(charset, bytes);
}

function decodeBEncodedText(text: string): Buffer | undefined {
  return PaddedBase64Pattern.test(text) ? Buffer.from(text, 'base64') : undefined;
}

function decodeQEncodedText(text: string): Buffer | undefined {
  const bytes: number[] = [];
  for (let index = 0; index < text.length; index++) {
    const character = text[index] ?? '';
    if (character === '_') bytes.push(0x20);
    else if (character === '=') {
      const hex = text.slice(index + 1, index + 3);
      if (!/^[0-9A-Fa-f]{2}$/.test(hex)) return undefined;
      bytes.push(parseInt(hex, 16));
      index += 2;
    } else if (character >= ' ' && character <= '~') bytes.push(character.charCodeAt(0));
    else return undefined;
  }
  return Buffer.from(bytes);
}

function decodeCharsetBytes(charset: string, bytes: Buffer): string | undefined {
  switch (charset.toLowerCase()) {
    case 'utf-8': return bytes.toString('utf8');
    case 'iso-8859-1': return bytes.toString('latin1');
    case 'us-ascii': return Array.from(bytes, byte => (byte < 0x80 ? String.fromCharCode(byte) : '\uFFFD')).join('');
    default:
      // Go rejects other charsets; TextDecoder throws for an unknown label or invalid bytes.
      try { return new TextDecoder(charset, { fatal: true }).decode(bytes); } catch { return undefined; }
  }
}

function formatMailbox(mailbox: { display: string; address: string }): string {
  return mailbox.display ? `${formatDisplayName(mailbox.display)} <${mailbox.address}>` : `<${mailbox.address}>`;
}

// Go's mail.Address.String keeps printable ASCII, space, and tab in a quoted string.
const QuotableDisplayName = /^[\t\x20-\x7E]*$/;
// RFC 2047 5(3) forbids these in a Q-encoded phrase, so Go B-encodes; Go misses the backslash.
const BEncodedDisplayNameCharacter = /["#$%&'(),.:;<>@[\\\]^`{|}~]/;
// An encoded word has at most 75 characters, 63 of them between "=?utf-8?q?" and "?=".
const MaxEncodedWordTextLength = 63;
// 45 bytes fill 60 Base64 characters, the most that fit in 63.
const MaxBEncodedWordBytes = 45;

function formatDisplayName(display: string): string {
  if (QuotableDisplayName.test(display)) return `"${display.replace(/["\\]/g, '\\$&')}"`;
  return BEncodedDisplayNameCharacter.test(display) ? encodeBWords(display) : encodeQWords(display);
}

// Go's mime.QEncoding splitting: never inside a character, and only where the next one would pass 63.
function encodeQWords(display: string): string {
  const words: string[] = [];
  let word = '';
  for (const character of display) {
    const isLiteral = character >= ' ' && character <= '~' && !'=?_'.includes(character);
    const encoded = isLiteral
      ? character.replace(' ', '_')
      : Array.from(Buffer.from(character, 'utf8'), byte => `=${byte.toString(16).toUpperCase().padStart(2, '0')}`).join('');
    if (word.length + encoded.length > MaxEncodedWordTextLength) {
      words.push(word);
      word = '';
    }
    word += encoded;
  }
  words.push(word);
  return words.map(text => `=?utf-8?q?${text}?=`).join(' ');
}

// Go's mime.BEncoding splitting: whole characters, at most 45 bytes per word.
function encodeBWords(display: string): string {
  const words: Buffer[][] = [[]];
  let wordByteLength = 0;
  for (const character of display) {
    const bytes = Buffer.from(character, 'utf8');
    if (wordByteLength + bytes.length > MaxBEncodedWordBytes) {
      words.push([]);
      wordByteLength = 0;
    }
    words.at(-1)?.push(bytes);
    wordByteLength += bytes.length;
  }
  return words.map(chunks => `=?utf-8?b?${Buffer.concat(chunks).toString('base64')}?=`).join(' ');
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

/**
 * Deterministic provider search identity for one Dex run and connector call.
 *
 * A valid `flowStartedAt` keeps the Go connector's derivation. An absent or
 * invalid one derives the identity from the key and run ID alone, which stays
 * stable across retries of the call within one run.
 *
 * @throws TypeError when `runId` is empty.
 */
export function rfcMessageIDAddress(idempotencyKey: string, runId: string, flowStartedAt?: Date | null): string {
  if (!runId) throw new TypeError('Dex run ID is required');
  // Callers such as dex-agents tool Steps may omit flowStartedAt; this non-numeric marker never equals Unix seconds.
  const runIdentity = flowStartedAt instanceof Date && Number.isFinite(flowStartedAt.getTime())
    ? `${runId}\0${Math.floor(flowStartedAt.getTime() / 1000)}`
    : `${runId}\0no-flow-start`;
  return `${createHash('sha256').update(`${idempotencyKey}\0${runIdentity}`).digest('hex')}@dex.superdurable.dev`;
}

/** Build the exact raw RFC 5322 request body shared by sends and replies. */
export function buildMIMEMessage(callId: string, idempotencyKey: string, messageIdAddress: string, sender: string, recipients: string[], subject: string, textBody: string, htmlBody = '', replyHeaders?: { inReplyTo: string; references: string }): Uint8Array {
  if (/[\r\n]/.test(sender + subject + messageIdAddress + recipients.join('') + (replyHeaders?.inReplyTo ?? '') + (replyHeaders?.references ?? ''))) throw new TypeError('MIME header contains a newline');
  const headers = [
    `From: ${sender}`, `To: ${recipients.join(', ')}`, `Subject: ${encodeSubject(subject)}`,
    `Message-ID: <${messageIdAddress}>`, `${DexMessageIDHeader}: <${messageIdAddress}>`,
  ];
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
