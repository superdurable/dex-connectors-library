// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { GeneratedMock as Mock, sendEmailDefault, sendEmailRetry } from '../../src/mock.js';
import { ConnectionName, WelcomeEmailFlow } from './flow.js';

/** Exercise the checked-in Flow against a real Dex Worker and Client. */
export async function verifyWelcomeEmailFlow(serverAddress: string): Promise<void> {
  const mock = new Mock(ConnectionName);
  const flow = new WelcomeEmailFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-resend-integration-'));
  const blobCache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, blobCache, { serverAddress });
  const client = new Client(registry, blobCache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const flowId = `resend-welcome-${randomUUID()}`;
    await client.startFlow(flow, flowId, { from: 'Acme <hello@example.test>', to: ['reader@example.test'], subject: 'Welcome', html: '<p>Hello</p>' });
    const result = await client.waitForFlow(flowId, 30_000);
    assert.equal(result.status, 'completed', result.errorMessage);
    assert.ok(result.singleOutput<{ id: string }>().id);
    assert.equal(mock.sendEmail().calls().length, 1);
    const retryId = `resend-welcome-retry-${randomUUID()}`;
    mock.sendEmail().forFlow(retryId).respond(
      sendEmailRetry({ kind: 'RATE_LIMIT', provider: 'resend', operation: 'sendEmail', message: 'provider rate limited the request' }, 10),
      sendEmailDefault(),
    );
    await client.startFlow(flow, retryId, { from: 'Acme <hello@example.test>', to: ['reader@example.test'], subject: 'Welcome', html: '<p>Hello</p>' });
    const retried = await client.waitForFlow(retryId, 30_000);
    assert.equal(retried.status, 'completed', retried.errorMessage);
    assert.deepEqual(mock.sendEmail().calls().filter(invocation => invocation.flowId === retryId).map(invocation => invocation.retry), [true, undefined]);
    mock.sendEmail().assertNoFailures();
  } finally {
    await client.close();
    await worker.close();
    blobCache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/integration.ts')) {
  verifyWelcomeEmailFlow(process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801').catch(error => { console.error(error); process.exitCode = 1; });
}
