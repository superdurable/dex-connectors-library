// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type BlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { FindCustomerByEmailInputSchema, newProjectConnection, type CustomerMatches } from '@superdurable/connector-atlassian-jira-service-management';
import { ConnectionName, FindCustomerFlow } from './flow/workflow.js';

async function startFindCustomer(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('input JSON file is required');
  const input = FindCustomerByEmailInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')));
  const project = await loadFromEnvironment();
  const flow = new FindCustomerFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-jsm-find-client-'));
  let cache: BlobCache | undefined;
  let client: Client | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
    const flowId = `jsm-find-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('customer lookup Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<CustomerMatches>())}\n`);
  } finally {
    try { await client?.close(); } finally { cache?.close(); await rm(directory, { recursive: true, force: true }); }
  }
}
try { await startFindCustomer(); } catch { console.error('Customer lookup Flow could not complete.'); process.exitCode = 1; }
