// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { GetDeploymentInputSchema, newProjectConnection, type Deployment } from '@superdurable/connector-vercel';
import { ConnectionName, VercelLookupFlow } from './flow/workflow.js';

async function startLookup(): Promise<void> {
  const filename = process.argv[2];
  if (!filename) throw new TypeError('Vercel lookup input JSON file is required');
  const input = GetDeploymentInputSchema.parse(JSON.parse(await readFile(filename, 'utf8')));
  const project = await loadFromEnvironment();
  const flow = new VercelLookupFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-vercel-starter-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    const flowId = `vercel-lookup-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('Vercel lookup Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<Deployment>())}\n`);
  } finally { await client.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}
try { await startLookup(); } catch { console.error('Vercel lookup Flow could not start or complete.'); process.exitCode = 1; }
