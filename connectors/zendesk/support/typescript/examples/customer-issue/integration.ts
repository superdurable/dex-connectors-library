// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock, searchTicketsRetry, searchTicketsSearched, searchTicketsAccessDenied } from '../../src/mock.js';
import type { SearchTicketsOutput } from '../../src/index.js';
import { CustomerIssueFlow, ConnectionName } from './flow.js';

/** Verify success, retry, and optional-branch failure across a real Dex Worker and Client. */
export async function verifyCustomerIssueFlow(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new CustomerIssueFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-zendesk-integration-'));
  const blobCache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, blobCache, { serverAddress });
  const client = new Client(registry, blobCache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const result: SearchTicketsOutput = { tickets: [] };
    const successId = `zendesk-success-${randomUUID()}`;
    mock.searchTickets().forFlow(successId).respond(searchTicketsSearched(result));
    await client.startFlow(flow, successId, { statuses: ['open'] });
    const success = await client.waitForFlow(successId, 30_000);
    assert.equal(success.status, 'completed', success.errorMessage);
    assert.deepEqual(success.singleOutput<SearchTicketsOutput>(), result);

    const retryId = `zendesk-retry-${randomUUID()}`;
    mock.searchTickets().forFlow(retryId).respond(searchTicketsRetry({ kind: 'RATE_LIMIT', provider: 'zendesk', operation: 'searchTickets', message: 'rate limited' }, 10), searchTicketsSearched(result));
    await client.startFlow(flow, retryId, { statuses: ['pending'] });
    const retry = await client.waitForFlow(retryId, 30_000);
    assert.equal(retry.status, 'completed', retry.errorMessage);
    assert.deepEqual(mock.searchTickets().calls().filter(call => call.flowId === retryId).map(call => call.retry), [true, undefined]);

    const rejectedId = `zendesk-rejected-${randomUUID()}`;
    mock.searchTickets().forFlow(rejectedId).respond(searchTicketsAccessDenied());
    await client.startFlow(flow, rejectedId, { statuses: ['open'] });
    const rejected = await client.waitForFlow(rejectedId, 30_000);
    assert.equal(rejected.status, 'failed');
    assert.match(rejected.errorMessage ?? '', /providerRejected/);
    mock.assertNoFailures();
  } finally {
    await client.close(); await worker.close(); blobCache.close(); await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) verifyCustomerIssueFlow(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
