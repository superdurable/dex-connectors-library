// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type BlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-close';
import { ConnectionName, CloseLookupFlow, type CloseLookupOutcome, type CloseLookupRequest } from './flow/workflow.js';

async function startCloseLookup(): Promise<void> {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new TypeError('DEX_FLOW_SERVICE_ADDRESS is required');
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('Close lookup input JSON file is required');
  const input = JSON.parse(await readFile(inputFile, 'utf8')) as CloseLookupRequest;
  const project = await loadFromEnvironment();
  const flow = new CloseLookupFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-close-client-'));
  let cache: BlobCache | undefined;
  let client: Client | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    client = new Client(new Registry([flow]), cache, { serverAddress: dexAddress });
    const flowId = `close-lookup-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('Close lookup Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<CloseLookupOutcome>())}\n`);
  } finally {
    try { await client?.close(); } finally { cache?.close(); await rm(directory, { recursive: true, force: true }); }
  }
}

try { await startCloseLookup(); } catch {
  console.error('Close lookup Flow could not start or complete.');
  process.exitCode = 1;
}
