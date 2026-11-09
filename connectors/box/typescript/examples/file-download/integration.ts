// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock } from '../../src/mock.js';
import { BoxDownloadFileFlow, ConnectionName } from './flow.js';

/** Run the checked-in Box Flow through a real Dex Worker and Client. */
export async function verifyBoxDownload(serverAddress: string): Promise<void> {
  const mock = new GeneratedMock(ConnectionName);
  const flow = new BoxDownloadFileFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-box-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowID = `box-ts-download-${randomUUID()}`;
    await client.startFlow(flow, flowID, { fileId: '12345' });
    const result = await client.waitForFlow(flowID, 30_000);
    assert.equal(result.status, 'completed', result.errorMessage);
    assert.deepEqual(result.singleOutput(), { fileId: '12345', contentBase64: 'SGVsbG8=', contentBytes: 5 });
    assert.equal(mock.downloadFile().calls().length, 1);
    mock.assertNoFailures();
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyBoxDownload(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => {
    console.error(error);
    process.exitCode = 1;
  });
}
