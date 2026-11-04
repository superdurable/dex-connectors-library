// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { ListInboxesInputSchema, newProjectConnection, type ListInboxesOutput } from '../../src/index.js';
import { ConnectionName, ListHiverInboxesFlow } from './flow.js';

async function start(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('Hiver list input JSON file is required');
  const input = ListInboxesInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')) as unknown);
  const project = await loadFromEnvironment();
  const flow = new ListHiverInboxesFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-hiver-inboxes-client-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    const flowId = `hiver-inboxes-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('Hiver inbox Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<ListInboxesOutput>())}\n`);
  } finally { await client.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}
try { await start(); } catch { console.error('Hiver inbox Flow could not start or complete.'); process.exitCode = 1; }
