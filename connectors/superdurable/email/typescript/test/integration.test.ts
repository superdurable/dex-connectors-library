// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { describe, expect, test } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { EmailClient } from '../src/index.js';

function call(operation: string): Call {
  let marker: unknown;
  const id = randomUUID();
  return { id, idempotencyKey: id, operation: { connectorId: 'email', operationId: operation }, connection: { provider: 'email', name: 'email' }, context: { attempt: 1, hasLastHeartbeatValue: () => marker !== undefined, getLastHeartbeatValue: () => marker, recordHeartbeat: async (value: unknown) => { marker = value; } } } as unknown as Call;
}

const environment = process.env;
const available = !!environment.EMAIL_CONNECTOR_TEST_CA_CERT;

let client: EmailClient;
if (available) client = new EmailClient({ imapHost: environment.EMAIL_CONNECTOR_TEST_IMAP_HOST || '127.0.0.1', imapPort: Number(environment.EMAIL_CONNECTOR_TEST_IMAPS_PORT), smtpHost: environment.EMAIL_CONNECTOR_TEST_SMTP_HOST || '127.0.0.1', smtpPort: Number(environment.EMAIL_CONNECTOR_TEST_SMTPS_PORT), fromAddress: environment.EMAIL_CONNECTOR_TEST_ADDRESS || '' }, { resolve: async () => ({ username: environment.EMAIL_CONNECTOR_TEST_USERNAME || '', password: new SecretString(environment.EMAIL_CONNECTOR_TEST_PASSWORD || '') }) }, { ca: readFileSync(environment.EMAIL_CONNECTOR_TEST_CA_CERT || '') });

describe.skipIf(!available)('GreenMail IMAPS and SMTPS', () => {
  test('send, search, read, flag, reply, and move through verified TLS', async () => {
    const subject = `Dex email integration ${randomUUID()}`;
    const sent = await client.sendMessage().invoke(call('sendMessage'), { to: [environment.EMAIL_CONNECTOR_TEST_ADDRESS || ''], subject, text: 'Hello from Dex email integration.' });
    expect(sent.kind).toBe('branch');
    if (sent.kind !== 'branch') return;
    expect(sent.branch).toBe('sent');
    expect(sent.receipt.providerObjectId).toBe(sent.value.messageId);
    let message = undefined;
    const deadline = Date.now() + 20_000;
    while (Date.now() < deadline) {
      const searched = await client.searchMessages().invoke(call('searchMessages'), { mailbox: 'INBOX', subjectContains: subject, limit: 10 });
      if (searched.kind === 'branch' && searched.branch === 'searched' && searched.value.messages?.length) { message = searched.value.messages[0]; break; }
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    expect(message).toBeDefined();
    if (!message) return;
    const read = await client.getMessage().invoke(call('getMessage'), { message: message.reference });
    expect(read.kind).toBe('branch');
    if (read.kind !== 'branch') return;
    expect(read.branch).toBe('found');
    expect(read.value.text).toContain('Hello from Dex email integration.');
    const flags = await client.setFlags().invoke(call('setFlags'), { message: message.reference, isSeen: true, isAnswered: true });
    expect(flags.kind).toBe('branch');
    if (flags.kind !== 'branch') return;
    expect(flags.branch).toBe('updated');
    const replied = await client.replyToMessage().invoke(call('replyToMessage'), { message: message.reference, text: 'Thanks for the message.' });
    expect(replied.kind).toBe('branch');
    if (replied.kind !== 'branch') return;
    expect(replied.branch).toBe('sent');
    expect(replied.value.inReplyTo).toBe(message.messageId);
    const archive = `DexArchive${randomUUID().replaceAll('-', '')}`;
    const setup = await client.openIMAP({ username: environment.EMAIL_CONNECTOR_TEST_USERNAME || '', password: new SecretString(environment.EMAIL_CONNECTOR_TEST_PASSWORD || '') });
    try { await setup.mailboxCreate(archive); } finally { await setup.logout(); }
    const moved = await client.moveMessage().invoke(call('moveMessage'), { message: message.reference, destinationMailbox: archive, messageId: message.messageId });
    expect(moved.kind).toBe('branch');
    if (moved.kind !== 'branch') return;
    expect(moved.branch).toBe('moved');
  });

  test('plaintext endpoints never receive credentials', async () => {
    const insecure = new EmailClient({ imapHost: environment.EMAIL_CONNECTOR_TEST_IMAP_HOST || '127.0.0.1', imapPort: Number(environment.EMAIL_CONNECTOR_TEST_IMAP_PORT), imapSecurity: 'startTLS', smtpHost: environment.EMAIL_CONNECTOR_TEST_SMTP_HOST || '127.0.0.1', smtpPort: Number(environment.EMAIL_CONNECTOR_TEST_SMTP_PORT), smtpSecurity: 'startTLS', fromAddress: environment.EMAIL_CONNECTOR_TEST_ADDRESS || '' }, { resolve: async () => ({ username: environment.EMAIL_CONNECTOR_TEST_USERNAME || '', password: new SecretString(environment.EMAIL_CONNECTOR_TEST_PASSWORD || '') }) }, { ca: readFileSync(environment.EMAIL_CONNECTOR_TEST_CA_CERT || '') });
    const search = await insecure.searchMessages().invoke(call('searchMessages'), {});
    expect(search.kind).toBe('branch');
    const send = await insecure.sendMessage().invoke(call('sendMessage'), { to: [environment.EMAIL_CONNECTOR_TEST_ADDRESS || ''], subject: 'No plaintext', text: 'No credentials sent.' });
    expect(send.kind).toBe('branch');
    if (send.kind === 'branch') expect(send.branch).toBe('providerRejected');
  });
});
