// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, SearchTicketsInputSchema, type SearchTicketsOutput } from '../../src/index.js';
import { ConnectionName, SearchTicketsFlow } from './flow/workflow.js';

const inputPath = process.argv[2];
if (!inputPath) throw new TypeError('A search input JSON file is required');
const input = SearchTicketsInputSchema.parse(JSON.parse(await readFile(inputPath, 'utf8')));
const project = await loadFromEnvironment();
const directory = await mkdtemp(join(tmpdir(), 'dex-zoho-desk-client-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
const flow = new SearchTicketsFlow(newProjectConnection(project, ConnectionName));
const client = new Client(new Registry([flow]), cache,
  { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `zoho-desk-search-${randomUUID()}`;
  await client.startFlow(flow, flowId, input);
  const result = await client.waitForFlow(flowId, 60_000);
  if (result.status !== 'completed') throw new Error('Zoho Desk search Flow did not complete');
  process.stdout.write(`${JSON.stringify(result.singleOutput<SearchTicketsOutput>())}\n`);
} finally {
  await client.close();
  cache.close();
  await rm(directory, { recursive: true, force: true });
}
