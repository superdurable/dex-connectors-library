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
import { ConnectionName, UserLookupFlow } from './flow.js';

/** Start one lookup and wait for its terminal status. */
export async function startEntraUserLookup(userKey: string): Promise<string> {
  if (!userKey.trim()) throw new TypeError('userKey is required');
  const project = await loadFromEnvironment();
  const flow = new UserLookupFlow(newProjectConnection(project, ConnectionName));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'entra-user-lookup-client-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS ?? '127.0.0.1:8801' });
  try {
    const flowId = `entra-user-lookup-${randomUUID()}`;
    await client.startFlow(flow, flowId, { userKey });
    return (await client.waitForFlow(flowId, 30_000)).status;
  } finally { await client.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  startEntraUserLookup(process.argv[2] ?? '').then(status => { process.stdout.write(`Entra lookup: ${status}\n`); })
    .catch(() => { process.stderr.write('Entra lookup failed\n'); process.exitCode = 1; });
}
