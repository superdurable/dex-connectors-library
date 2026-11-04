// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ImapFlow, FetchMessageObject, MessageStructureObject, SearchObject } from 'imapflow';
import { simpleParser } from 'mailparser';
import { Parser } from 'htmlparser2';
import { newQueryBranch, newQueryRetry, newMutationBranch, newMutationRetry, newMutationUncertain, type Call, type Failure, type MutationAttempt, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { SearchMessagesInput, SearchMessagesOutput, GetMessageInput, Message, MessageReference, MessageSummary, EmailAddress, SendMessageInput, SentMessage, ReplyToMessageInput, MoveMessageInput, MovedMessage, SetFlagsInput, MessageFlags, Attachment, Credentials } from './generated/models.js';
import { SearchMessagesDefinition, GetMessageDefinition, SendMessageDefinition, ReplyToMessageDefinition, MoveMessageDefinition, SetFlagsDefinition } from './generated/definitions.js';
import { classifyMailError, CredentialFailure, hasControls, isBareAddress, MailFailure, type EmailClient } from './client.js';
import { submitMail, type OutgoingMail } from './smtp.js';

function failure(operation: string, mailFailure: MailFailure): Failure { return { kind: mailFailure.kind, provider: 'email', operation, message: mailFailure.message }; }
function invalid(message: string): MailFailure { return new MailFailure('rejected', 'VALIDATION', message); }
function zeroSummary(): MessageSummary { return { reference: { mailbox: '', uidValidity: 0, uid: 0 }, from: { address: '' }, subject: '', receivedAt: '0001-01-01T00:00:00Z', isSeen: false, isFlagged: false, isAnswered: false, sizeBytes: 0, hasAttachments: false }; }
const zeroMessage: Message = { ...zeroSummary(), text: '', textSource: '' };
const zeroSearch: SearchMessagesOutput = { mailbox: '', uidValidity: 0, messages: [], totalMatched: 0, hasMore: false };
const zeroSent: SentMessage = { messageId: '', from: '', recipients: [], subject: '' };
const zeroMove: MovedMessage = { destination: { mailbox: '', uidValidity: 0, uid: 0 } };
const zeroFlags: MessageFlags = { message: { mailbox: '', uidValidity: 0, uid: 0 }, isSeen: false, isFlagged: false, isAnswered: false };
function queryFailure<T>(operation: string, error: unknown, zero: T, supportsNotFound: boolean): QueryAttempt<T> {
  if (error instanceof CredentialFailure) return newQueryBranch('defect', zero, {}, failure(operation, error));
  const classified = classifyMailError(error, 'IMAP');
  if (classified.outcome === 'retry') return newQueryRetry(failure(operation, classified));
  const branch = classified.outcome === 'notFound' && supportsNotFound ? 'notFound' : classified.outcome === 'invalidResponse' ? 'invalidResponse' : 'providerRejected';
  return newQueryBranch(branch, zero, {}, failure(operation, classified));
}
function mutationFailure<T>(operation: string, error: unknown, zero: T, supportsNotFound: boolean, supportsInvalidResponse = false): MutationAttempt<T> {
  if (error instanceof CredentialFailure) return newMutationBranch('defect', zero, {}, failure(operation, error));
  const classified = classifyMailError(error, 'IMAP');
  if (classified.outcome === 'retry' || classified.outcome === 'invalidResponse' && (operation === 'moveMessage' || operation === 'setFlags')) return newMutationRetry(failure(operation, classified));
  return newMutationBranch(classified.outcome === 'notFound' && supportsNotFound ? 'notFound' : classified.outcome === 'invalidResponse' && supportsInvalidResponse ? 'invalidResponse' : 'providerRejected', zero, {}, failure(operation, classified));
}
function validateMailbox(name: string): void {
  if (!name || Buffer.byteLength(name) > 255 || hasControls(name) || name.trim() !== name || /[*%]/.test(name)) throw invalid('mailbox must be a name of at most 255 bytes without controls, wildcards, or surrounding spaces');
}
function validateReference(reference: MessageReference): void {
  if (!reference) throw invalid('message is required');
  validateMailbox(reference.mailbox);
  if (!Number.isInteger(reference.uidValidity) || reference.uidValidity < 1 || reference.uidValidity > 0xffffffff || !Number.isInteger(reference.uid) || reference.uid < 1 || reference.uid > 0xffffffff) throw invalid('message UID and UIDVALIDITY are required');
}
async function select(client: ImapFlow, mailbox: string, readOnly: boolean, expected = 0): Promise<number> {
  let selected;
  try { selected = await client.mailboxOpen(mailbox, { readOnly }); }
  catch (error) {
    const responseCode = String((error as { serverResponseCode?: string; responseCode?: string })?.serverResponseCode || (error as { responseCode?: string })?.responseCode || '').toUpperCase();
    if (responseCode === 'NONEXISTENT' || responseCode === 'TRYCREATE') throw new MailFailure('notFound', 'NOT_FOUND', 'the mailbox does not exist');
    throw error;
  }
  const uidValidity = Number(selected.uidValidity);
  if (!Number.isSafeInteger(uidValidity) || uidValidity < 1 || uidValidity > 0xffffffff) throw new MailFailure('invalidResponse', 'PROTOCOL', 'the IMAP server selected the mailbox without a valid UIDVALIDITY');
  if (expected && uidValidity !== expected) throw new MailFailure('notFound', 'NOT_FOUND', "the mailbox's UIDVALIDITY changed; search again");
  return uidValidity;
}
async function close(client: ImapFlow): Promise<void> { try { await client.logout(); } catch { client.close(); /* The completed operation's outcome is already decided. */ } }
function addresses(values: { name?: string; address?: string }[] | undefined): EmailAddress[] { return (values || []).filter(value => !!value.address).slice(0, 50).map(value => ({ ...(value.name ? { name: truncateBytes(value.name, 512) } : {}), address: value.address! })); }
function selectedTextPart(node?: MessageStructureObject): { part: MessageStructureObject; source: 'plain' | 'html' } | undefined {
  for (const mediaType of ['text/plain', 'text/html'] as const) {
    const visit = (part?: MessageStructureObject): MessageStructureObject | undefined => {
      if (!part || isAttachmentPart(part)) return undefined;
      if (part.type.toLowerCase() === mediaType) return part;
      for (const child of part.childNodes || []) { const selected = visit(child); if (selected) return selected; }
      return undefined;
    };
    const selected = visit(node);
    if (selected) return { part: selected, source: mediaType === 'text/plain' ? 'plain' : 'html' };
  }
  return undefined;
}

function isAttachmentPart(node: MessageStructureObject): boolean {
  return node.disposition?.toLowerCase() === 'attachment' || !!(node.dispositionParameters?.filename || node.parameters?.name) || node.type.toLowerCase() === 'message/rfc822';
}

function collectAttachments(node?: MessageStructureObject, path = ''): Attachment[] {
  if (!node) return [];
  const children = node.type.toLowerCase() === 'message/rfc822' ? [] : (node.childNodes || []).flatMap((child, index) => collectAttachments(child, path ? `${path}.${index + 1}` : `${index + 1}`));
  const fileName = node.dispositionParameters?.filename || node.parameters?.name || '';
  const mediaType = node.type.toLowerCase();
  if (!isAttachmentPart(node)) return children;
  return [{ part: node.part || path || '1', ...(fileName ? { fileName: truncateBytes(fileName, 255) } : {}), mediaType, encodedSizeBytes: node.size || 0, ...((node.disposition || '').toLowerCase() === 'inline' ? { isInline: true } : {}) }, ...children];
}

function extractTextFromHTML(document: string): string {
  let content = '';
  let skippedDepth = 0;
  const blockTags = new Set(['br', 'p', 'div', 'li', 'tr', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'blockquote', 'table']);
  const skipTags = new Set(['script', 'style', 'head', 'title']);
  const parser = new Parser({
    onopentag(name) { if (skipTags.has(name)) skippedDepth++; else if (blockTags.has(name)) content += '\n'; },
    onclosetag(name) { if (skipTags.has(name)) skippedDepth = Math.max(0, skippedDepth - 1); else if (name !== 'br' && blockTags.has(name)) content += '\n'; },
    ontext(run) {
      if (skippedDepth) return;
      const words = run.trim().split(/\s+/u).filter(Boolean);
      if (!words.length) { if (run && content && !/\s$/u.test(content)) content += ' '; return; }
      if (/^\s/u.test(run) && content && !/\s$/u.test(content)) content += ' ';
      content += words.join(' ');
      if (/\s$/u.test(run)) content += ' ';
    },
  }, { decodeEntities: true });
  parser.end(document);
  const lines = content.split('\n').map(line => line.trim());
  const collapsed: string[] = [];
  for (const line of lines) { if (line || collapsed.length && collapsed.at(-1)) collapsed.push(line); }
  return collapsed.join('\n').trim();
}
function summary(mailbox: string, uidValidity: number, fetched: FetchMessageObject): MessageSummary {
  if (!Number.isInteger(fetched.uid) || fetched.uid < 1 || fetched.uid > 0xffffffff || !fetched.envelope || !fetched.internalDate || fetched.size === undefined || !Number.isSafeInteger(fetched.size) || fetched.size < 0 || !fetched.flags) throw new MailFailure('invalidResponse', 'PROTOCOL', "the IMAP server's FETCH response omitted required fields");
  const envelope = fetched.envelope;
  const flags = [...fetched.flags].map(value => value.toLowerCase());
  const received = new Date(fetched.internalDate);
  if (Number.isNaN(received.valueOf())) throw new MailFailure('invalidResponse', 'PROTOCOL', 'the IMAP server returned an invalid internal date');
  const result: MessageSummary = { reference: { mailbox, uidValidity, uid: fetched.uid }, from: addresses(envelope.from)[0] || { address: '' }, subject: truncateBytes(envelope.subject || '', 998), receivedAt: goTime(received), sizeBytes: fetched.size, hasAttachments: collectAttachments(fetched.bodyStructure).length > 0, isSeen: flags.includes('\\seen'), isFlagged: flags.includes('\\flagged'), isAnswered: flags.includes('\\answered') };
  if (envelope.messageId) result.messageId = envelope.messageId.replace(/^<|>$/g, '');
  if (envelope.inReplyTo) result.inReplyTo = envelope.inReplyTo.replace(/^<|>$/g, '');
  if (envelope.replyTo?.length) result.replyTo = addresses(envelope.replyTo);
  if (envelope.to?.length) result.to = addresses(envelope.to);
  if (envelope.cc?.length) result.cc = addresses(envelope.cc);
  if (envelope.date) { const sent = new Date(envelope.date); if (!Number.isNaN(sent.valueOf())) result.sentAt = goTime(sent); }
  return result;
}
function goTime(date: Date): string { return date.toISOString().replace('.000Z', 'Z'); }
function truncateBytes(value: string, limit: number): string { let text = ''; let size = 0; for (const character of value) { const bytes = Buffer.byteLength(character); if (size + bytes > limit) break; text += character; size += bytes; } return text; }
function validateSearch(input: SearchMessagesInput): void {
  if (input.mailbox) validateMailbox(input.mailbox);
  if (input.limit && (!Number.isInteger(input.limit) || input.limit < 1 || input.limit > 50)) throw invalid('limit must be from 1 through 50');
  for (const value of [input.from, input.to, input.subjectContains]) if (value && (value.trim() !== value || Buffer.byteLength(value) > 256 || hasControls(value))) throw invalid('search text must be at most 256 bytes without controls or surrounding spaces');
  for (const value of [input.sinceDate, input.beforeDate]) if (value && !isCalendarDate(value)) throw invalid('search date must be YYYY-MM-DD');
  if (input.sinceDate && input.beforeDate && input.beforeDate <= input.sinceDate) throw invalid('beforeDate must be later than sinceDate');
  if (input.olderThanUid && (!Number.isInteger(input.olderThanUid) || input.olderThanUid > 0xffffffff || !Number.isInteger(input.uidValidity) || !input.uidValidity || input.olderThanUid <= 1)) throw invalid('uidValidity is required with olderThanUid greater than 1');
  if (!input.olderThanUid && input.uidValidity) throw invalid('uidValidity is used only with olderThanUid');
}
function isCalendarDate(value: string): boolean { if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false; const date = new Date(`${value}T00:00:00Z`); return !Number.isNaN(date.valueOf()) && date.toISOString().startsWith(value); }
function validateText(text: string): void { if (!text?.trim() || Buffer.byteLength(text) > 512 * 1024 || [...text].some(character => character !== '\n' && character !== '\r' && character !== '\t' && hasControls(character))) throw invalid('text must be nonempty UTF-8 text of at most 524288 bytes'); }
function validateSubject(subject: string): void { if (Buffer.byteLength(subject || '') > 998 || hasControls(subject || '')) throw invalid('subject must be one line of at most 998 bytes'); }
function validateAddressList(addresses: string[] | null | undefined, required = false): void { if ((required && !addresses?.length) || addresses?.some(address => !isBareAddress(address))) throw invalid('recipients must be bare ASCII addresses'); }
function isMessageIdentifier(value: string): boolean { return !!value && Buffer.byteLength(value) <= 250 && value.includes('@') && [...value].every(character => { const code = character.charCodeAt(0); return code > 32 && code < 127 && character !== '<' && character !== '>'; }); }
function messageID(call: Call, sender: string): string { const key = call.idempotencyKey || call.id; if (!/^[A-Za-z0-9-]{1,128}$/.test(key)) throw invalid('the idempotency key cannot form a Message-ID'); return `dex-${key.toLowerCase()}@${sender.split('@').at(-1)!.toLowerCase()}`; }
function sender(client: EmailClient, credentials: Credentials): string { const address = client.config.fromAddress || credentials.username; if (!isBareAddress(address)) throw invalid('fromAddress is blank and username is not a complete address'); return address; }
function uniqueAddresses(values: string[]): string[] { const seen = new Set<string>(); return values.filter(value => { const key = value.toLowerCase(); if (seen.has(key)) return false; seen.add(key); return true; }); }
function sentValue(message: OutgoingMail): SentMessage { return { messageId: message.messageId, from: message.from, recipients: uniqueAddresses([...message.to, ...message.cc, ...message.bcc]), subject: message.subject, ...(message.inReplyTo ? { inReplyTo: message.inReplyTo } : {}), ...(message.references?.length ? { references: message.references } : {}), sentAt: goTime(message.date) }; }
async function claimSubmission(call: Call, value: SentMessage, operation: string): Promise<MutationAttempt<SentMessage> | undefined> {
  try { if (call.context.hasLastHeartbeatValue()) { const earlier = { ...value }; delete earlier.sentAt; return newMutationUncertain(earlier, failure(operation, new MailFailure('rejected', 'TRANSPORT', 'an earlier attempt may have submitted the message, so it is not submitted again')), { providerObjectId: value.messageId }); } }
  catch { return newMutationUncertain(value, failure(operation, new MailFailure('rejected', 'TRANSPORT', 'the submission checkpoint is unreadable')), { providerObjectId: value.messageId }); }
  try { await call.context.recordHeartbeat({ emailSubmittedCallId: call.id }); }
  catch { return newMutationRetry(failure(operation, new MailFailure('retry', 'AVAILABILITY', 'Dex did not record the submission checkpoint; nothing was submitted'))); }
  return undefined;
}
async function finishSubmission(client: EmailClient, credentials: Credentials, call: Call, operation: string, message: OutgoingMail): Promise<MutationAttempt<SentMessage>> {
  const value = sentValue(message);
  const claim = await claimSubmission(call, value, operation);
  if (claim) return claim;
  const result = await submitMail(client, credentials, message);
  if (result.outcome === 'accepted') return newMutationBranch('sent', value, { providerObjectId: value.messageId });
  if (result.outcome === 'uncertain') return newMutationUncertain(value, failure(operation, result.failure), { providerObjectId: value.messageId });
  try { await call.context.recordHeartbeat(undefined); } catch { /* A retained marker prevents a duplicate submission. */ }
  if (result.failure.outcome === 'retry') return newMutationRetry(failure(operation, result.failure));
  return newMutationBranch('providerRejected', zeroSent, {}, failure(operation, result.failure));
}

/** Search one mailbox without marking messages seen. */
export class SearchMessagesOperation {
  /** Bind one authenticated email client for searchMessages. */
  constructor(private readonly client: EmailClient) {}
  /** Return the manifest identity and retry defaults for searchMessages. */
  definition() { return SearchMessagesDefinition; }
  /** Invoke searchMessages with current credentials and classified protocol outcomes. */
  async invoke(call: Call, input: SearchMessagesInput): Promise<QueryAttempt<SearchMessagesOutput>> {
    try { validateSearch(input); } catch (error) { return newQueryBranch('defect', zeroSearch, {}, failure('searchMessages', error as MailFailure)); }
    let session: ImapFlow | undefined;
    try {
      const credentials = await this.client.resolveCredentials(call);
      session = await this.client.openIMAP(credentials);
      const mailbox = input.mailbox || 'INBOX';
      const uidValidity = await select(session, mailbox, true);
      if (input.uidValidity && uidValidity !== input.uidValidity) return newQueryBranch('providerRejected', zeroSearch, {}, failure('searchMessages', new MailFailure('rejected', 'CONFLICT', 'the mailbox UIDVALIDITY changed; start the search again')));
      const query: SearchObject = {};
      if (input.from || input.to || input.subjectContains) query.header = { ...(input.from ? { from: input.from } : {}), ...(input.to ? { to: input.to } : {}), ...(input.subjectContains ? { subject: input.subjectContains } : {}) };
      if (input.sinceDate) query.since = new Date(`${input.sinceDate}T00:00:00Z`);
      if (input.beforeDate) query.before = new Date(`${input.beforeDate}T00:00:00Z`);
      if (input.isUnseen) query.seen = false;
      if (input.olderThanUid) query.uid = `1:${input.olderThanUid - 1}`;
      const found = await session.search(query, { uid: true });
      if (!Array.isArray(found)) throw new MailFailure('rejected', 'PROVIDER_REJECTION', 'the IMAP server refused SEARCH');
      const uids = [...new Set(found)].sort((a, b) => b - a);
      const page = uids.slice(0, input.limit || 10);
      const messages: MessageSummary[] = [];
      for (const uid of page) { const fetched = await session.fetchOne(String(uid), { envelope: true, flags: true, size: true, internalDate: true, bodyStructure: true }, { uid: true }); if (fetched) messages.push(summary(mailbox, uidValidity, fetched)); }
      return newQueryBranch('searched', { mailbox, uidValidity, messages, totalMatched: uids.length, hasMore: uids.length > page.length, ...(uids.length > page.length && page.length ? { nextOlderThanUid: page[page.length - 1] } : {}) });
    } catch (error) { return queryFailure('searchMessages', error, zeroSearch, false); }
    finally { if (session) await close(session); }
  }
}

async function readMessage(session: ImapFlow, reference: MessageReference, readText: boolean): Promise<Message> {
  await select(session, reference.mailbox, true, reference.uidValidity);
  const fetched = await session.fetchOne(String(reference.uid), { envelope: true, flags: true, size: true, internalDate: true, bodyStructure: true, headers: ['references'] }, { uid: true });
  if (!fetched) throw new MailFailure('notFound', 'NOT_FOUND', 'no message in the mailbox has the UID');
  const result: Message = { ...summary(reference.mailbox, reference.uidValidity, fetched), text: '', textSource: 'none' };
  const references = (fetched.headers?.toString('utf8').match(/<[^>]+>/g) || []).map(value => value.slice(1, -1)).filter(isMessageIdentifier).slice(-20);
  if (references.length) result.references = references;
  const attachments = collectAttachments(fetched.bodyStructure);
  if (attachments.length) result.attachments = attachments.slice(0, 50);
  if (attachments.length > 50) result.hasMoreAttachments = true;
  const chosenTextPart = readText ? selectedTextPart(fetched.bodyStructure) : undefined;
  if (chosenTextPart) {
    try {
      const partNumber = chosenTextPart.part.part || '1';
      const body = await session.fetchOne(String(reference.uid), { bodyParts: [{ key: partNumber, start: 0, maxLength: 1024 * 1024 }] }, { uid: true });
      if (!body) throw new MailFailure('notFound', 'NOT_FOUND', 'the message was removed while it was read');
      const encoded = body.bodyParts?.get(partNumber);
      if (!encoded) throw new MailFailure('invalidResponse', 'PROTOCOL', 'the IMAP server omitted the selected text part');
      const charset = chosenTextPart.part.parameters?.charset;
      const encoding = chosenTextPart.part.encoding;
      const mimeHeaders = [`Content-Type: ${chosenTextPart.part.type.toLowerCase()}${charset && /^[\w.-]+$/.test(charset) ? `; charset=${charset}` : ''}`,
        `Content-Transfer-Encoding: ${encoding && /^[\w-]+$/.test(encoding) ? encoding : '8bit'}`];
      const parsed = await simpleParser(Buffer.concat([Buffer.from(`${mimeHeaders.join('\r\n')}\r\n\r\n`), encoded]));
      const text = chosenTextPart.source === 'plain' ? parsed.text || '' : extractTextFromHTML(typeof parsed.html === 'string' ? parsed.html : '');
      result.text = truncateBytes(text.replace(/\r\n/g, '\n'), 64 * 1024);
      result.textSource = chosenTextPart.source;
      if ((chosenTextPart.part.size || 0) > 1024 * 1024 || Buffer.byteLength(result.text) < Buffer.byteLength(text)) result.isTextTruncated = true;
    } catch (error) { if (error instanceof MailFailure) throw error; throw new MailFailure('invalidResponse', 'PROTOCOL', 'the IMAP message body could not be parsed'); }
  }
  return result;
}

/** Read a message by stable IMAP reference. */
export class GetMessageOperation {
  /** Bind one authenticated email client for getMessage. */
  constructor(private readonly client: EmailClient) {}
  /** Return the manifest identity and retry defaults for getMessage. */
  definition() { return GetMessageDefinition; }
  /** Invoke getMessage with current credentials and classified protocol outcomes. */
  async invoke(call: Call, input: GetMessageInput): Promise<QueryAttempt<Message>> {
    try { validateReference(input.message); } catch (error) { return newQueryBranch('defect', zeroMessage, {}, failure('getMessage', error as MailFailure)); }
    let session: ImapFlow | undefined;
    try { session = await this.client.openIMAP(await this.client.resolveCredentials(call)); const message = await readMessage(session, input.message, true); return newQueryBranch('found', message, { providerObjectId: message.messageId || '' }); }
    catch (error) { return queryFailure('getMessage', error, zeroMessage, true); }
    finally { if (session) await close(session); }
  }
}

/** Submit one new message with a Dex heartbeat checkpoint. */
export class SendMessageOperation {
  /** Bind one authenticated email client for sendMessage. */
  constructor(private readonly client: EmailClient) {}
  /** Return the manifest identity and retry defaults for sendMessage. */
  definition() { return SendMessageDefinition; }
  /** Use the stable Dex Call ID for the operation identity. */
  idempotencyKey(callId: string): string { return callId; }
  /** Invoke sendMessage with current credentials and classified protocol outcomes. */
  async invoke(call: Call, input: SendMessageInput): Promise<MutationAttempt<SentMessage>> {
    try {
      validateAddressList(input.to, true); validateAddressList(input.cc); validateAddressList(input.bcc);
      if ((input.to?.length || 0) + (input.cc?.length || 0) + (input.bcc?.length || 0) > 50) throw invalid('at most 50 recipients are allowed');
      if (input.replyTo && !isBareAddress(input.replyTo)) throw invalid('replyTo must be a bare ASCII address');
      validateSubject(input.subject); validateText(input.text);
      const credentials = await this.client.resolveCredentials(call);
      const from = sender(this.client, credentials);
      const message: OutgoingMail = { from, fromName: this.client.config.fromName, to: input.to || [], cc: input.cc || [], bcc: input.bcc || [], replyTo: input.replyTo, subject: input.subject, text: input.text, messageId: messageID(call, from), date: new Date(Math.floor(this.client.now().getTime() / 1000) * 1000) };
      return finishSubmission(this.client, credentials, call, 'sendMessage', message);
    } catch (error) { return newMutationBranch('defect', zeroSent, {}, failure('sendMessage', error as MailFailure)); }
  }
}

/** Reply to a source message and preserve its thread identifiers. */
export class ReplyToMessageOperation {
  /** Bind one authenticated email client for replyToMessage. */
  constructor(private readonly client: EmailClient) {}
  /** Return the manifest identity and retry defaults for replyToMessage. */
  definition() { return ReplyToMessageDefinition; }
  /** Use the stable Dex Call ID for the operation identity. */
  idempotencyKey(callId: string): string { return callId; }
  /** Invoke replyToMessage with current credentials and classified protocol outcomes. */
  async invoke(call: Call, input: ReplyToMessageInput): Promise<MutationAttempt<SentMessage>> {
    try { validateReference(input.message); validateText(input.text); validateAddressList(input.cc); if ((input.cc?.length || 0) > 50) throw invalid('at most 50 cc recipients are allowed'); } catch (error) { return newMutationBranch('defect', zeroSent, {}, failure('replyToMessage', error as MailFailure)); }
    let session: ImapFlow | undefined;
    try {
      const credentials = await this.client.resolveCredentials(call);
      let from: string, id: string;
      try { from = sender(this.client, credentials); id = messageID(call, from); }
      catch (error) { return newMutationBranch('defect', zeroSent, {}, failure('replyToMessage', error as MailFailure)); }
      try { if (call.context.hasLastHeartbeatValue()) return newMutationUncertain({ messageId: id, from, recipients: [], subject: '' }, failure('replyToMessage', new MailFailure('rejected', 'TRANSPORT', 'an earlier attempt may have submitted the reply')), { providerObjectId: id }); }
      catch { return newMutationUncertain({ messageId: id, from, recipients: [], subject: '' }, failure('replyToMessage', new MailFailure('rejected', 'TRANSPORT', 'the submission checkpoint is unreadable')), { providerObjectId: id }); }
      session = await this.client.openIMAP(credentials);
      const source = await readMessage(session, input.message, false);
      const primary = source.replyTo?.length ? source.replyTo : [source.from];
      const to = uniqueAddresses(primary.map(value => value.address).filter(value => isBareAddress(value) && value.toLowerCase() !== from.toLowerCase()));
      if (!to.length) return newMutationBranch('invalidResponse', zeroSent, {}, failure('replyToMessage', new MailFailure('rejected', 'PROTOCOL', 'the source message has no usable reply address')));
      const cc = uniqueAddresses([...(input.isReplyAll ? [...(source.to || []), ...(source.cc || [])].map(value => value.address) : []), ...(input.cc || [])].filter(value => isBareAddress(value) && value.toLowerCase() !== from.toLowerCase() && !to.some(primaryAddress => primaryAddress.toLowerCase() === value.toLowerCase())));
      if (to.length + cc.length > 50) return newMutationBranch('invalidResponse', zeroSent, {}, failure('replyToMessage', new MailFailure('rejected', 'PROTOCOL', 'the reply would have too many recipients')));
      const subject = source.subject.trim();
      const sourceID = isMessageIdentifier(source.messageId || '') ? source.messageId : undefined;
      const message: OutgoingMail = { from, fromName: this.client.config.fromName, to, cc, bcc: [], subject: truncateBytes(/^re:/i.test(subject) ? subject : `Re: ${subject}`.trim(), 998), text: input.text, messageId: id, inReplyTo: sourceID, references: sourceID ? [...(source.references || []), sourceID].slice(-20) : [], date: new Date(Math.floor(this.client.now().getTime() / 1000) * 1000) };
      await close(session); session = undefined;
      return finishSubmission(this.client, credentials, call, 'replyToMessage', message);
    } catch (error) { return mutationFailure('replyToMessage', error, zeroSent, true, true); }
    finally { if (session) await close(session); }
  }
}

/** Move a message once, finding a prior move by Message-ID. */
export class MoveMessageOperation {
  /** Bind one authenticated email client for moveMessage. */
  constructor(private readonly client: EmailClient) {}
  /** Return the manifest identity and retry defaults for moveMessage. */
  definition() { return MoveMessageDefinition; }
  /** Use the stable Dex Call ID for the operation identity. */
  idempotencyKey(callId: string): string { return callId; }
  /** Invoke moveMessage with current credentials and classified protocol outcomes. */
  async invoke(call: Call, input: MoveMessageInput): Promise<MutationAttempt<MovedMessage>> {
    try { validateReference(input.message); validateMailbox(input.destinationMailbox); if (input.messageId && !isMessageIdentifier(input.messageId)) throw invalid('messageId must be a valid Message-ID'); if (input.destinationMailbox === input.message.mailbox || input.destinationMailbox.toUpperCase() === 'INBOX' && input.message.mailbox.toUpperCase() === 'INBOX') throw invalid('destinationMailbox must differ from source'); } catch (error) { return newMutationBranch('defect', zeroMove, {}, failure('moveMessage', error as MailFailure)); }
    let session: ImapFlow | undefined;
    try {
      session = await this.client.openIMAP(await this.client.resolveCredentials(call));
      if (!session.capabilities?.has('MOVE') && !session.capabilities?.has('UIDPLUS')) return newMutationBranch('providerRejected', zeroMove, {}, failure('moveMessage', new MailFailure('rejected', 'PROVIDER_REJECTION', 'the IMAP server supports neither MOVE nor UIDPLUS, so a safe move is unavailable')));
      await select(session, input.message.mailbox, false, input.message.uidValidity);
      const source = await session.fetchOne(String(input.message.uid), { envelope: true }, { uid: true });
      if (!source) {
        if (!input.messageId) return newMutationBranch('notFound', zeroMove, {}, failure('moveMessage', new MailFailure('notFound', 'NOT_FOUND', 'no message in source mailbox has the UID')));
        const uidValidity = await select(session, input.destinationMailbox, true);
        const found = await session.search({ header: { 'Message-ID': `<${input.messageId}>` } }, { uid: true });
        if (!Array.isArray(found)) throw new MailFailure('rejected', 'PROVIDER_REJECTION', 'the IMAP server refused SEARCH');
        for (const uid of found.sort((a, b) => b - a)) {
          const message = await session.fetchOne(String(uid), { envelope: true }, { uid: true });
          if (message && message.envelope?.messageId?.replace(/^<|>$/g, '') === input.messageId) return newMutationBranch('moved', { destination: { mailbox: input.destinationMailbox, uidValidity, uid }, wasAlreadyMoved: true }, { providerObjectId: input.messageId });
        }
        return newMutationBranch('notFound', zeroMove, {}, failure('moveMessage', new MailFailure('notFound', 'NOT_FOUND', 'the message is in neither mailbox')));
      }
      if (input.messageId && source.envelope?.messageId?.replace(/^<|>$/g, '') !== input.messageId) return newMutationBranch('notFound', zeroMove, {}, failure('moveMessage', new MailFailure('notFound', 'NOT_FOUND', 'the message at the UID has a different Message-ID')));
      const moved = await session.messageMove(String(input.message.uid), input.destinationMailbox, { uid: true });
      if (!moved) throw this.client.consumeIMAPCommandFailure(session) || (session.usable === false ? new MailFailure('retry', 'TRANSPORT', 'the IMAP connection was lost during MOVE') : new MailFailure('rejected', 'PROVIDER_REJECTION', 'the IMAP server did not move the message'));
      return newMutationBranch('moved', { destination: { mailbox: input.destinationMailbox, uidValidity: Number(moved.uidValidity || 0), uid: moved.uidMap?.get(input.message.uid) || 0 } }, { providerObjectId: input.messageId || '' });
    } catch (error) { return mutationFailure('moveMessage', error, zeroMove, true); }
    finally { if (session) await close(session); }
  }
}

/** Set or clear selected flags to absolute values. */
export class SetFlagsOperation {
  /** Bind one authenticated email client for setFlags. */
  constructor(private readonly client: EmailClient) {}
  /** Return the manifest identity and retry defaults for setFlags. */
  definition() { return SetFlagsDefinition; }
  /** Use the stable Dex Call ID for the operation identity. */
  idempotencyKey(callId: string): string { return callId; }
  /** Invoke setFlags with current credentials and classified protocol outcomes. */
  async invoke(call: Call, input: SetFlagsInput): Promise<MutationAttempt<MessageFlags>> {
    try { validateReference(input.message); if ([input.isSeen, input.isFlagged, input.isAnswered].every(value => value === undefined)) throw invalid('at least one flag is required'); } catch (error) { return newMutationBranch('defect', zeroFlags, {}, failure('setFlags', error as MailFailure)); }
    let session: ImapFlow | undefined;
    try {
      session = await this.client.openIMAP(await this.client.resolveCredentials(call));
      await select(session, input.message.mailbox, false, input.message.uidValidity);
      const read = async (): Promise<MessageFlags> => {
        const fetched = await session!.fetchOne(String(input.message.uid), { flags: true }, { uid: true });
        if (!fetched) throw new MailFailure('notFound', 'NOT_FOUND', 'no message in the mailbox has the UID');
        const flags = [...(fetched.flags || [])].map(value => value.toLowerCase());
        return { message: input.message, isSeen: flags.includes('\\seen'), isFlagged: flags.includes('\\flagged'), isAnswered: flags.includes('\\answered') };
      };
      const current = await read();
      const changes = [[input.isSeen, current.isSeen, '\\Seen'], [input.isFlagged, current.isFlagged, '\\Flagged'], [input.isAnswered, current.isAnswered, '\\Answered']] as const;
      const add = changes.filter(([wanted, has]) => wanted === true && !has).map(([, , flag]) => flag);
      const remove = changes.filter(([wanted, has]) => wanted === false && has).map(([, , flag]) => flag);
      if (!add.length && !remove.length) return newMutationBranch('updated', { ...current, wasAlreadyApplied: true });
      if (add.length && !await session.messageFlagsAdd(String(input.message.uid), add, { uid: true })) throw this.client.consumeIMAPCommandFailure(session) || new MailFailure('rejected', 'PROVIDER_REJECTION', 'the IMAP server did not accept the requested flag addition');
      if (remove.length && !await session.messageFlagsRemove(String(input.message.uid), remove, { uid: true })) throw this.client.consumeIMAPCommandFailure(session) || new MailFailure('rejected', 'PROVIDER_REJECTION', 'the IMAP server did not accept the requested flag removal');
      const updated = await read();
      if (Object.entries({ isSeen: input.isSeen, isFlagged: input.isFlagged, isAnswered: input.isAnswered }).some(([key, wanted]) => wanted !== undefined && updated[key as keyof MessageFlags] !== wanted)) return newMutationBranch('providerRejected', updated, {}, failure('setFlags', new MailFailure('rejected', 'PROVIDER_REJECTION', 'the IMAP server did not keep the requested flags')));
      return newMutationBranch('updated', updated);
    } catch (error) { return mutationFailure('setFlags', error, zeroFlags, true); }
    finally { if (session) await close(session); }
  }
}
