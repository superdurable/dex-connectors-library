// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { GeneratedMock } from '../../src/generated/mock.js';
import { ConnectionName, RecordRefundFlow, type RefundOutcome } from '../../examples/record-refund/flow/workflow.js';

it('runs the refund example through a real Dex Worker and Client', async () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('DEX_FLOW_SERVICE_ADDRESS must name a running dexcli dev server');
  const mock = new GeneratedMock(ConnectionName);
  const flow = new RecordRefundFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-postgresql-example-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: address });
  const client = new Client(registry, cache, { serverAddress: address });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `postgresql-refund-${randomUUID()}`;
    await client.startFlow(flow, flowId, { orderId: 'order-88213', amountUsd: '250.00', externalReference: 'SUP-88213' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<RefundOutcome>()).toMatchObject({ status: 'recorded', row: { amount_usd: '250.00', order_id: 'order-88213' } });
    mock.assertNoFailures();
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 90_000);
