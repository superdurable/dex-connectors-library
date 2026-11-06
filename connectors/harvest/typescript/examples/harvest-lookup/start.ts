// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { ListProjectsInputSchema, newProjectConnection, type TimeEntryPage } from '../../src/index.js';
import { ConnectionName, HarvestLookupFlow } from './flow.js';

try {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('Harvest lookup input file is required');
  const input = ListProjectsInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')));
  const project = await loadFromEnvironment();
  const flow = new HarvestLookupFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-harvest-client-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    const flowId = `harvest-lookup-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('Harvest lookup Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<TimeEntryPage>())}\n`);
  } finally {
    await client.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
} catch {
  console.error('Harvest lookup Flow could not start or complete.');
  process.exitCode = 1;
}
