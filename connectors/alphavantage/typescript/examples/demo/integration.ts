// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { getGlobalQuoteRetry, GeneratedMock } from '../../src/alphavantagemock.js';
import type { GlobalQuoteOutput } from '../../src/index.js';
import { AlphaVantageQuoteDemo, ConnectionName } from './flow.js';

/** Exercise the checked-in Flow through a real Dex Worker, Client, and retry boundary. */
export async function verifyAlphaVantageDemo(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new AlphaVantageQuoteDemo(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-alphavantage-integration-'));
  const blobCache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, blobCache, { serverAddress });
  const client = new Client(registry, blobCache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const successId = `alphavantage-success-${randomUUID()}`;
    await client.startFlow(flow, successId, { symbol: 'IBM' });
    const success = await client.waitForFlow(successId, 30_000);
    assert.equal(success.status, 'completed', success.errorMessage);
    assert.deepEqual(success.singleOutput<GlobalQuoteOutput>(), { symbol: 'IBM', price: '255.25', latestTradingDay: '2026-10-28' });
    assert.equal(mock.getGlobalQuote().calls()[0]?.input.symbol, 'IBM');

    const retryId = `alphavantage-retry-${randomUUID()}`;
    mock.getGlobalQuote().forFlow(retryId).respond(
      getGlobalQuoteRetry({ provider: 'alphavantage', operation: 'getGlobalQuote', kind: 'RATE_LIMIT', message: 'provider rate limited quote' }, 10),
    );
    await client.startFlow(flow, retryId, { symbol: 'MSFT' });
    const retried = await client.waitForFlow(retryId, 30_000);
    assert.equal(retried.status, 'completed', retried.errorMessage);
    assert.deepEqual(mock.getGlobalQuote().calls().filter(call => call.flowId === retryId).map(call => call.retry), [true, undefined]);
    mock.assertNoFailures();
  } finally {
    await client.close();
    await worker.close();
    blobCache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyAlphaVantageDemo(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
}
