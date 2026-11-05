// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock, getStatementResultQueryStillRunning } from '../../src/mock.js';
import { AccountUsageSummaryFlow, ConnectionName } from './flow.js';

/** Verify the checked-in Flow across a real Dex Worker, Client, and persistence boundary. */
export async function verifyAccountUsageSummaryFlow(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new AccountUsageSummaryFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-snowflake-integration-'));
  const blobCache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, blobCache, { serverAddress });
  const client = new Client(registry, blobCache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });

    const completedId = `snowflake-completed-${randomUUID()}`;
    await client.startFlow(flow, completedId, { accountId: 'acct_003', since: '2026-01-01' });
    const completed = await client.waitForFlow(completedId, 30_000);
    assert.equal(completed.status, 'completed', completed.errorMessage);
    assert.equal(completed.singleOutput<{ status: string }>().status, 'completed');
    assert.deepEqual(mock.submitStatement().calls()[0]?.input.parameters, ['acct_003', '2026-01-01']);

    const canceledId = `snowflake-canceled-${randomUUID()}`;
    mock.getStatementResult().forFlow(canceledId).respond(getStatementResultQueryStillRunning());
    await client.startFlow(flow, canceledId, { accountId: 'acct_004', since: '2026-02-01' });
    const canceled = await client.waitForFlow(canceledId, 30_000);
    assert.equal(canceled.status, 'completed', canceled.errorMessage);
    assert.equal(canceled.singleOutput<{ status: string }>().status, 'canceled');
    assert.equal(mock.cancelStatement().calls().filter(call => call.flowId === canceledId).length, 1);
    mock.assertNoFailures();
  } finally {
    await client.close();
    await worker.close();
    blobCache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyAccountUsageSummaryFlow(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
}
