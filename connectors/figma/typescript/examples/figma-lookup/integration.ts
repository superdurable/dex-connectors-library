// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock, getFileInvalidKey, getFileRetry, getFileDefaultAnswer } from '../../src/mock.js';
import { ConnectionName, FigmaLookupFlow, type FigmaLookupOutcome } from './flow.js';

/** Run the checked-in Flow through a real Dex Worker and Client. */
export async function verifyFigmaLookup(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new FigmaLookupFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-figma-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });

    const successId = `figma-success-${randomUUID()}`;
    await client.startFlow(flow, successId, { fileKey: 'fileA' });
    const success = await client.waitForFlow(successId, 30_000);
    assert.equal(success.status, 'completed', success.errorMessage);
    const outcome = success.singleOutput<FigmaLookupOutcome>();
    assert.equal(outcome.name, 'Meridian Design');
    assert.equal(outcome.comments?.[0]?.message, 'Review the header.');
    assert.equal(mock.getFile().calls().length, 1);
    assert.equal(mock.getFileComments().calls().length, 1);

    const retryId = `figma-retry-${randomUUID()}`;
    mock.getFile().forFlow(retryId).respond(getFileRetry({ kind: 'RATE_LIMIT', provider: 'figma', operation: 'getFile', message: 'rate limit' }, 10), getFileDefaultAnswer());
    await client.startFlow(flow, retryId, { fileKey: 'fileB' });
    const retried = await client.waitForFlow(retryId, 30_000);
    assert.equal(retried.status, 'completed', retried.errorMessage);
    assert.deepEqual(mock.getFile().calls().filter(call => call.flowId === retryId).map(call => call.retry), [true, undefined]);

    const rejectedId = `figma-rejected-${randomUUID()}`;
    mock.getFile().forFlow(rejectedId).respond(getFileInvalidKey());
    await client.startFlow(flow, rejectedId, { fileKey: 'fileC' });
    const rejected = await client.waitForFlow(rejectedId, 30_000);
    assert.equal(rejected.status, 'failed');
    assert.match(rejected.errorMessage ?? '', /providerRejected/);
    mock.assertNoFailures();
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyFigmaLookup(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => {
    console.error(error);
    process.exitCode = 1;
  });
}
