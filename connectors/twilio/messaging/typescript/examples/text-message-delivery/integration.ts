// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock, getMessageRetry, getMessageDeliveredText } from '../../src/mock.js';
import { ConnectionName, TextMessageDeliveryFlow } from './flow.js';

/** Verify the checked-in Flow over real Dex Worker, Client, and retry boundaries. */
export async function verifyTextMessageDelivery(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new TextMessageDeliveryFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-twilio-message-'));
  const blobCache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, blobCache, { serverAddress });
  const client = new Client(registry, blobCache, { serverAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const flowId = `twilio-message-${randomUUID()}`;
    mock.getMessage().forFlow(flowId).respond(getMessageRetry({ kind: 'RATE_LIMIT', provider: 'twilio', operation: 'getMessage', message: 'rate limited' }, 10), getMessageDeliveredText());
    await client.startFlow(flow, flowId, { to: '+14155550123', body: 'Ready for pickup.' });
    const result = await client.waitForFlow(flowId, 30_000);
    assert.equal(result.status, 'completed', result.errorMessage);
    assert.equal(result.singleOutput<{ status: string }>().status, 'delivered');
    assert.equal(mock.sendMessage().calls().length, 1);
    assert.equal(mock.getMessage().calls().length, 2);
    mock.assertNoFailures();
  } finally { await client.close(); await worker.close(); blobCache.close(); await rm(directory, { recursive: true, force: true }); }
}

if (process.argv[1]?.endsWith('/integration.ts')) verifyTextMessageDelivery(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
