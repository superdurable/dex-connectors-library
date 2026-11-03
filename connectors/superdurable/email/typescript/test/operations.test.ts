// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, test, vi } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import type { ImapFlow } from 'imapflow';
import { EmailClient, MailFailure } from '../src/index.js';
import { classifyMailError } from '../src/client.js';

function fakeCall(operation: string, prior?: unknown): { call: Call; heartbeats: unknown[] } {
  const heartbeats: unknown[] = [];
  const call = { id: 'email-test-call', idempotencyKey: 'email-test-call', connection: { provider: 'email', name: 'email' }, operation: { connectorId: 'email', operationId: operation }, context: { attempt: prior === undefined ? 1 : 2, hasLastHeartbeatValue: () => prior !== undefined, getLastHeartbeatValue: () => prior, recordHeartbeat: async (value: unknown) => { heartbeats.push(value); } } } as unknown as Call;
  return { call, heartbeats };
}
function client() { return new EmailClient({ imapHost: 'localhost', imapPort: 39993, smtpHost: 'localhost', smtpPort: 34655, fromAddress: 'support@example.com' }, { resolve: async () => ({ username: 'support@example.com', password: new SecretString('test-password') }) }, { now: () => new Date('2026-01-28T10:05:00Z') }); }
function fakeIMAP(overrides: Record<string, unknown> = {}): ImapFlow {
  return { capabilities: new Map([['MOVE', true]]), mailboxOpen: async () => ({ uidValidity: 42n }), search: async () => [100, 101], fetchOne: async (uid: string) => ({ uid: Number(uid), flags: new Set<string>(), envelope: { messageId: `message-${uid}@example.com`, subject: 'Hello', from: [{ address: 'jane@example.com' }], replyTo: [{ address: 'jane@example.com' }] }, internalDate: new Date('2026-01-28T10:00:00Z'), size: 100 }), logout: async () => {}, close: () => {}, messageMove: async () => ({ uidValidity: 43n, uidMap: new Map([[100, 50]]) }), messageFlagsAdd: async () => true, messageFlagsRemove: async () => true, ...overrides } as unknown as ImapFlow;
}

describe('email operation decisions', () => {
  test('rejects malformed config and credentials without contacting a provider', async () => {
    expect(() => new EmailClient({ imapHost: 'https://mail.example.com', smtpHost: 'smtp.example.com' }, { resolve: async () => ({ username: 'x', password: new SecretString('x') }) })).toThrow();
    const email = new EmailClient({ imapHost: 'localhost', smtpHost: 'localhost' }, { resolve: async () => { throw new Error('secret text'); } });
    const result = await email.searchMessages().invoke(fakeCall('searchMessages').call, {});
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') { expect(result.branch).toBe('defect'); expect(result.failure?.message).not.toContain('secret text'); }
  });

  test('search returns newest-first bounded summaries and rejects stale pagination', async () => {
    const email = client();
    vi.spyOn(email, 'openIMAP').mockResolvedValue(fakeIMAP());
    const searched = await email.searchMessages().invoke(fakeCall('searchMessages').call, { limit: 1, subjectContains: 'Hello' });
    expect(searched.kind).toBe('branch');
    if (searched.kind === 'branch') { expect(searched.branch).toBe('searched'); expect(searched.value.messages?.[0]?.reference.uid).toBe(101); expect(searched.value.nextOlderThanUid).toBe(101); }
    const stale = await email.searchMessages().invoke(fakeCall('searchMessages').call, { olderThanUid: 100, uidValidity: 43 });
    expect(stale.kind).toBe('branch');
    if (stale.kind === 'branch') expect(stale.branch).toBe('providerRejected');
  });

  test('IMAP authentication is terminal and transport loss retries', async () => {
    const email = client();
    vi.spyOn(email, 'openIMAP').mockRejectedValueOnce(new MailFailure('rejected', 'AUTHENTICATION', 'the IMAP login was refused')).mockRejectedValueOnce(Object.assign(new Error('connection reset'), { code: 'ECONNRESET' }));
    const rejected = await email.getMessage().invoke(fakeCall('getMessage').call, { message: { mailbox: 'INBOX', uidValidity: 42, uid: 100 } });
    expect(rejected.kind).toBe('branch');
    if (rejected.kind === 'branch') expect(rejected.branch).toBe('providerRejected');
    const retry = await email.getMessage().invoke(fakeCall('getMessage').call, { message: { mailbox: 'INBOX', uidValidity: 42, uid: 100 } });
    expect(retry.kind).toBe('retry');
  });

  test('getMessage selects notFound for missing UID and move finds prior move', async () => {
    const email = client();
    vi.spyOn(email, 'openIMAP').mockResolvedValueOnce(fakeIMAP({ fetchOne: async () => false })).mockResolvedValueOnce(fakeIMAP({ fetchOne: async () => false, search: async () => [] }));
    const reference = { mailbox: 'INBOX', uidValidity: 42, uid: 100 };
    const missing = await email.getMessage().invoke(fakeCall('getMessage').call, { message: reference });
    expect(missing.kind).toBe('branch');
    if (missing.kind === 'branch') expect(missing.branch).toBe('notFound');
    const move = await email.moveMessage().invoke(fakeCall('moveMessage').call, { message: reference, destinationMailbox: 'Archive', messageId: 'message-100@example.com' });
    expect(move.kind).toBe('branch');
    if (move.kind === 'branch') expect(move.branch).toBe('notFound');
  });

  test('setFlags reports an already applied value and a rejected write', async () => {
    const email = client();
    const reference = { mailbox: 'INBOX', uidValidity: 42, uid: 100 };
    vi.spyOn(email, 'openIMAP').mockResolvedValueOnce(fakeIMAP({ fetchOne: async () => ({ uid: 100, flags: new Set(['\\Seen']) }) })).mockResolvedValueOnce(fakeIMAP());
    const already = await email.setFlags().invoke(fakeCall('setFlags').call, { message: reference, isSeen: true });
    expect(already.kind).toBe('branch');
    if (already.kind === 'branch') { expect(already.branch).toBe('updated'); expect(already.value.wasAlreadyApplied).toBe(true); }
    const refused = await email.setFlags().invoke(fakeCall('setFlags').call, { message: reference, isSeen: true });
    expect(refused.kind).toBe('branch');
    if (refused.kind === 'branch') expect(refused.branch).toBe('providerRejected');
  });

  test('send validates recipients and never connects after a prior submission claim', async () => {
    const email = client();
    const invalid = await email.sendMessage().invoke(fakeCall('sendMessage').call, { to: ['wrong'], subject: 'Hello', text: 'Hello' });
    expect(invalid.kind).toBe('branch');
    if (invalid.kind === 'branch') expect(invalid.branch).toBe('defect');
    const prior = await email.sendMessage().invoke(fakeCall('sendMessage', { emailSubmittedCallId: 'earlier' }).call, { to: ['jane@example.com'], subject: 'Hello', text: 'Hello' });
    expect(prior.kind).toBe('uncertain');
    if (prior.kind === 'uncertain') { expect(prior.value.messageId).toBe('dex-email-test-call@example.com'); expect(prior.receipt.providerObjectId).toBe(prior.value.messageId); }
  });

  test('reply with missing source selects notFound before SMTP', async () => {
    const email = client();
    vi.spyOn(email, 'openIMAP').mockResolvedValue(fakeIMAP({ fetchOne: async () => false }));
    const reply = await email.replyToMessage().invoke(fakeCall('replyToMessage').call, { message: { mailbox: 'INBOX', uidValidity: 42, uid: 100 }, text: 'Thanks' });
    expect(reply.kind).toBe('branch');
    if (reply.kind === 'branch') expect(reply.branch).toBe('notFound');
  });
});

test('project connection maps every configured mail endpoint and SMTP credential override', async () => {
  const { newProjectConnection } = await import('../src/index.js');
  const project = { configuration: { connections: [{ connectorId: 'email', connectionName: 'email', provider: 'email', configuration: { imapHost: 'imap.example.com', imapPort: 1993, imapSecurity: 'startTLS', smtpHost: 'smtp.example.com', smtpPort: 1465, smtpSecurity: 'implicitTLS', fromAddress: 'support@example.com', fromName: 'Acme Support' } }] }, resolveCredential: async () => ({ username: 'imap-user', password: 'imap-secret', smtp_username: 'smtp-user', smtp_password: 'smtp-secret' }) };
  const connection = newProjectConnection(project as never, 'email');
  const email = connection.operations as EmailClient;
  expect(email.config).toMatchObject({ imapPort: 1993, imapSecurity: 'startTLS', smtpPort: 1465, fromAddress: 'support@example.com', fromName: 'Acme Support' });
  const credentials = await email.resolveCredentials(fakeCall('sendMessage').call);
  expect(credentials.smtpUsername).toBe('smtp-user');
  expect(credentials.smtpPassword?.reveal()).toBe('smtp-secret');
});

test('getMessage decodes a plain MIME body and preserves a safe attachment summary', async () => {
  const email = client();
  const source = Buffer.from('From: Jane <jane@example.com>\r\nSubject: Hello\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nHello from Jane.\r\n');
  vi.spyOn(email, 'openIMAP').mockResolvedValue(fakeIMAP({ fetchOne: async () => ({ uid: 100, flags: new Set<string>(), envelope: { messageId: 'message-100@example.com', subject: 'Hello', from: [{ address: 'jane@example.com' }] }, internalDate: new Date('2026-01-28T10:00:00Z'), size: source.length, headers: Buffer.from('References: <older@example.com>\r\n'), source, bodyStructure: { type: 'text/plain', size: 17 } }) }));
  const read = await email.getMessage().invoke(fakeCall('getMessage').call, { message: { mailbox: 'INBOX', uidValidity: 42, uid: 100 } });
  expect(read.kind).toBe('branch');
  if (read.kind === 'branch') { expect(read.branch).toBe('found'); expect(read.value.text).toContain('Hello from Jane.'); expect(read.value.references).toEqual(['older@example.com']); expect(read.receipt.providerObjectId).toBe('message-100@example.com'); }
});

test('move returns destination UID and receipt after a safe IMAP MOVE', async () => {
  const email = client();
  vi.spyOn(email, 'openIMAP').mockResolvedValue(fakeIMAP());
  const moved = await email.moveMessage().invoke(fakeCall('moveMessage').call, { message: { mailbox: 'INBOX', uidValidity: 42, uid: 100 }, destinationMailbox: 'Archive', messageId: 'message-100@example.com' });
  expect(moved.kind).toBe('branch');
  if (moved.kind === 'branch') { expect(moved.branch).toBe('moved'); expect(moved.value.destination).toEqual({ mailbox: 'Archive', uidValidity: 43, uid: 50 }); expect(moved.receipt.providerObjectId).toBe('message-100@example.com'); }
});

test('move refuses a server without MOVE or UIDPLUS before changing flags', async () => {
  const email = client();
  const fake = fakeIMAP({ capabilities: new Map() });
  const move = vi.spyOn(fake, 'messageMove');
  vi.spyOn(email, 'openIMAP').mockResolvedValue(fake);
  const result = await email.moveMessage().invoke(fakeCall('moveMessage').call, { message: { mailbox: 'INBOX', uidValidity: 42, uid: 100 }, destinationMailbox: 'Archive' });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') expect(result.branch).toBe('providerRejected');
  expect(move).not.toHaveBeenCalled();
});

test.each([
  [{ responseStatus: 'NO', responseCode: 'AUTHENTICATIONFAILED', command: 'LOGIN' }, 'rejected', 'AUTHENTICATION'],
  [{ responseStatus: 'NO', responseCode: 'UNAVAILABLE' }, 'retry', 'AVAILABILITY'],
  [{ responseStatus: 'NO', responseCode: 'LIMIT' }, 'retry', 'RATE_LIMIT'],
  [{ responseStatus: 'NO', responseCode: 'NONEXISTENT' }, 'notFound', 'NOT_FOUND'],
  [{ responseStatus: 'BAD' }, 'rejected', 'PROTOCOL'],
  [{ code: 'ECONNREFUSED' }, 'retry', 'TRANSPORT'],
] as const)('classifies IMAP protocol status without provider text', (providerError, outcome, kind) => {
  const classified = classifyMailError(providerError, 'IMAP');
  expect(classified.outcome).toBe(outcome);
  expect(classified.kind).toBe(kind);
  expect(classified.message).not.toContain('secret');
});
