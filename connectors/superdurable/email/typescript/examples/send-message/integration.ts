// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createServer } from 'node:tls';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { GeneratedMock, sendMessageRecipientRejected, sendMessageNewCustomerMessage } from '../../src/mock.js';
import { EmailClient, newConnection } from '../../src/index.js';
import { ConnectionName, SendMessageFlow } from './flow.js';

/** Verify the runnable Flow through a real Dex Worker and Client. */
export async function verifySendMessageFlow(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new SendMessageFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-email-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const input = { to: ['jane@example.com'], subject: 'Hello', text: 'Hello Jane' };
    const sentId = `email-sent-${randomUUID()}`;
    mock.sendMessage().forFlow(sentId).respond(sendMessageNewCustomerMessage());
    await client.startFlow(flow, sentId, input);
    const sent = await client.waitForFlow(sentId, 30_000);
    assert.equal(sent.status, 'completed', sent.errorMessage);
    const rejectedId = `email-rejected-${randomUUID()}`;
    mock.sendMessage().forFlow(rejectedId).respond(sendMessageRecipientRejected());
    await client.startFlow(flow, rejectedId, input);
    const rejected = await client.waitForFlow(rejectedId, 30_000);
    assert.equal(rejected.status, 'failed');
    assert.match(rejected.errorMessage || '', /providerRejected/);
    mock.assertNoFailures();
  } finally { await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}

/** Verify a transient SMTP refusal clears the real Dex checkpoint before retry. */
export async function verifyTransientSMTPRetry(serverAddress: string): Promise<void> {
  const directory = await mkdtemp(join(tmpdir(), 'dex-email-retry-'));
  const certificatePath = join(directory, 'server.crt');
  const keyPath = join(directory, 'server.key');
  execFileSync('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-subj', '/CN=localhost', '-addext', 'subjectAltName=DNS:localhost', '-keyout', keyPath, '-out', certificatePath], { stdio: 'ignore' });
  const certificate = await readFile(certificatePath);
  const key = await readFile(keyPath);
  let mailAttempts = 0;
  let dataCount = 0;
  const smtpServer = createServer({ cert: certificate, key }, socket => {
    socket.write('220 localhost ESMTP ready\r\n');
    let buffer = '';
    let inData = false;
    socket.on('data', chunk => {
      buffer += chunk.toString('utf8');
      for (;;) {
        const end = buffer.indexOf('\r\n');
        if (end < 0) break;
        const line = buffer.slice(0, end);
        buffer = buffer.slice(end + 2);
        if (inData) {
          if (line === '.') { dataCount++; inData = false; socket.write('250 accepted\r\n'); }
        } else if (line.startsWith('EHLO')) socket.write('250-localhost\r\n250-AUTH PLAIN\r\n250 SIZE 0\r\n');
        else if (line.startsWith('AUTH PLAIN')) socket.write('235 authenticated\r\n');
        else if (line.startsWith('MAIL FROM')) { mailAttempts++; socket.write(mailAttempts === 1 ? '451 try again\r\n' : '250 ok\r\n'); }
        else if (line.startsWith('RCPT TO')) socket.write('250 ok\r\n');
        else if (line === 'DATA') { inData = true; socket.write('354 send content\r\n'); }
        else if (line === 'QUIT') socket.write('221 bye\r\n');
      }
    });
  });
  await new Promise<void>((resolve, reject) => { smtpServer.once('error', reject); smtpServer.listen(36301, '127.0.0.1', resolve); });
  const cache = openBlobCache({ directory: join(directory, 'blobs'), maxBytes: 1 << 30 });
  const email = new EmailClient({ imapHost: 'localhost', smtpHost: 'localhost', smtpPort: 36301, fromAddress: 'support@example.com' },
    { resolve: async () => ({ username: 'support@example.com', password: new SecretString('test-password') }) }, { ca: certificate });
  const flow = new SendMessageFlow(newConnection(email, { provider: 'email', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const flowId = `email-retry-${randomUUID()}`;
    await client.startFlow(flow, flowId, { to: ['customer@example.com'], subject: 'Retry after 451', text: 'One DATA only' });
    const finished = await client.waitForFlow(flowId, 30_000);
    assert.equal(finished.status, 'completed', finished.errorMessage);
    assert.equal(mailAttempts, 2);
    assert.equal(dataCount, 1);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await new Promise<void>(resolve => smtpServer.close(() => resolve()));
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:36300';
  verifySendMessageFlow(address).then(() => verifyTransientSMTPRetry(address)).catch(error => { console.error(error); process.exitCode = 1; });
}
