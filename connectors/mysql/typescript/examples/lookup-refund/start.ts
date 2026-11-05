// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, type RowSet } from '../../src/index.js';
import { ConnectionName, LookupRefundFlow } from './flow.js';

async function startRefundLookup(): Promise<void> {
  const orderId = process.argv[2];
  if (!orderId?.trim()) throw new TypeError('orderId is required');
  const project = await loadFromEnvironment();
  const flow = new LookupRefundFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-mysql-refund-client-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    const flowId = `mysql-refund-${randomUUID()}`;
    await client.startFlow(flow, flowId, { orderId });
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('MySQL refund Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<RowSet>())}\n`);
  } finally { await client.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}
try { await startRefundLookup(); } catch { console.error('MySQL refund Flow could not complete.'); process.exitCode = 1; }
