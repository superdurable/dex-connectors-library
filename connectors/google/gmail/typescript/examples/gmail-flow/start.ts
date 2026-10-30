// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, GmailExampleFlow } from './flow.js';

/** Start one read in the checked-in Gmail Flow, then wait for its terminal status. */
export async function startGmailExampleRead(messageId: string): Promise<string> {
  if (!messageId?.trim()) throw new TypeError('Gmail message ID is required');
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  const flow = new GmailExampleFlow(connection);
  const registry = new Registry([flow]);
  const cacheDirectory = await mkdtemp(join(tmpdir(), 'gmail-example-client-cache-'));
  const cache = openBlobCache({ directory: cacheDirectory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS ?? '127.0.0.1:8801' });
  try {
    const flowId = `gmail-example-${randomUUID()}`;
    await client.startFlow(flow, flowId, { action: 'read', messageId });
    const result = await client.waitForFlow(flowId, 30_000);
    return result.status;
  } finally {
    await client.close();
    cache.close();
    await rm(cacheDirectory, { recursive: true, force: true });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  startGmailExampleRead(process.argv[2] ?? '').then(status => { process.stdout.write(`Gmail Flow status: ${status}\n`); })
    .catch(() => { process.stderr.write('Gmail example read failed\n'); process.exitCode = 1; });
}
