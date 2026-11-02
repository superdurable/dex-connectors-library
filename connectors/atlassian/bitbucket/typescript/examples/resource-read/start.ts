// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type BlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { ListRepositoriesInputSchema, newProjectConnection, type RepositoryPage } from '@superdurable/connector-atlassian-bitbucket';
import { ConnectionName, ResourceReadFlow } from './flow/workflow.js';

async function startResourceRead(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('Bitbucket resource input JSON file is required');
  const input = ListRepositoriesInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')));
  const project = await loadFromEnvironment();
  const flow = new ResourceReadFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-bitbucket-resource-client-'));
  let cache: BlobCache | undefined;
  let client: Client | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:21270' });
    const flowId = `bitbucket-resource-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('Bitbucket resource Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<RepositoryPage>())}\n`);
  } finally {
    try { await client?.close(); } finally {
      cache?.close();
      await rm(directory, { recursive: true, force: true });
    }
  }
}

try { await startResourceRead(); } catch {
  console.error('Bitbucket resource Flow could not start or complete.');
  process.exitCode = 1;
}
