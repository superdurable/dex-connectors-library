// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { Mock, changeMessageVisibilityChanged, receiveMessagesReceived } from '../../src/sqsmock.js';
import { ConnectionName, QueueConsumerFlow, type ConsumerOutcome } from './flow.js';

/** Run this checked-in Flow through real Dex Worker, Client, and persistence. */
export async function verifyQueueConsumer(serverAddress: string): Promise<void> {
  const mock = new Mock(ConnectionName);
  const flow = new QueueConsumerFlow(mock.connection());
  const directory = await mkdtemp(join(tmpdir(), 'dex-sqs-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const paidId = `sqs-paid-${randomUUID()}`;
    await client.startFlow(flow, paidId, {});
    const paid = await client.waitForFlow(paidId, 30_000);
    assert.equal(paid.status, 'completed', paid.errorMessage);
    assert.deepEqual(paid.singleOutput<ConsumerOutcome>(), { status: 'processed', messageId: 'order-1042', isAcknowledgementConfirmed: true });
    assert.equal(mock.deleteMessage().calls().filter(call => call.flowId === paidId).length, 1);

    const invalidId = `sqs-invalid-${randomUUID()}`;
    mock.receiveMessages().forFlow(invalidId).respond(receiveMessagesReceived({ queueUrl: 'https://sqs.eu-west-1.amazonaws.com/123456789012/orders',
      messages: [{ messageId: 'order-invalid', receiptHandle: 'receipt-invalid', body: 'invalid', md5OfBody: 'fedb2d84cafe20862cb4399751a8a7e3', receiveCount: 1 }] }));
    mock.changeMessageVisibility().forFlow(invalidId).respond(changeMessageVisibilityChanged({
      queueUrl: 'https://sqs.eu-west-1.amazonaws.com/123456789012/orders', messageId: 'order-invalid',
      visibilityTimeoutSeconds: 0, visibleAfter: '2026-10-25T09:00:02Z',
    }));
    await client.startFlow(flow, invalidId, {});
    const invalid = await client.waitForFlow(invalidId, 30_000);
    assert.equal(invalid.status, 'completed', invalid.errorMessage);
    assert.deepEqual(invalid.singleOutput<ConsumerOutcome>(), { status: 'rejected', messageId: 'order-invalid', isAcknowledgementConfirmed: true });
    assert.equal(mock.changeMessageVisibility().calls().filter(call => call.flowId === invalidId)[0]?.input.visibilityTimeoutSeconds, 0);
    mock.assertNoFailures();
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyQueueConsumer(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
}
