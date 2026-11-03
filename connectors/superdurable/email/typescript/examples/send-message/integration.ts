// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock, sendMessageRecipientRejected, sendMessageNewCustomerMessage } from '../../src/mock.js';
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

if (process.argv[1]?.endsWith('/integration.ts')) verifySendMessageFlow(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
