// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { GeneratedMock, queryCompleted, queryRetry, queryProviderRejected, emptyRowSet } from '../../src/generated/mock.js';
import { ConnectionName, LookupRefundFlow } from '../../examples/lookup-refund/flow.js';

it('runs the checked-in refund Flow across a real Dex retry and failure boundary', async () => {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new Error('DEX_FLOW_SERVICE_ADDRESS must name a running dexcli dev server');
  const mock = new GeneratedMock(ConnectionName);
  const flow = new LookupRefundFlow(mock.connection());
  const directory = await mkdtemp(join(tmpdir(), 'dex-mysql-refund-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `mysql-refund-${randomUUID()}`;
    const result = { columns: [{ name: 'order_id', typeName: 'VARCHAR' }], rows: [{ order_id: '88213' }] };
    mock.query().forFlow(successId).respond(queryRetry({ kind: 'AVAILABILITY', provider: 'mysql', operation: 'query', message: 'temporary outage' }), queryCompleted(result));
    await client.startFlow(flow, successId, { orderId: '88213' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<typeof result>().rows[0]?.order_id).toBe('88213');
    expect(mock.query().calls().filter(record => record.flowId === successId).map(record => record.retry)).toEqual([true, undefined]);
    const rejectedId = `mysql-refund-rejected-${randomUUID()}`;
    mock.query().forFlow(rejectedId).respond(queryProviderRejected(emptyRowSet, { kind: 'AUTHENTICATION', provider: 'mysql', operation: 'query', message: 'bad account' }));
    await client.startFlow(flow, rejectedId, { orderId: '88214' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    mock.assertNoFailures();
  } finally { await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}, 90_000);
