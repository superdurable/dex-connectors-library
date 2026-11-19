// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type BlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { QueryRowsInputSchema, newProjectConnection, type RowSet } from '@superdurable/connector-microsoft-sql-server';
import { ConnectionName, QueryRowsFlow } from './flow/workflow.js';

async function startQueryRows(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('SQL Server query input JSON file is required');
  const input = QueryRowsInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')));
  const project = await loadFromEnvironment();
  const flow = new QueryRowsFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-sql-server-query-client-'));
  let cache: BlobCache | undefined;
  let client: Client | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
    const flowId = `sql-server-query-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('SQL Server query Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<RowSet>())}\n`);
  } finally {
    try { await client?.close(); } finally {
      cache?.close();
      await rm(directory, { recursive: true, force: true });
    }
  }
}

try { await startQueryRows(); } catch {
  console.error('SQL Server query Flow could not start or complete.');
  process.exitCode = 1;
}
