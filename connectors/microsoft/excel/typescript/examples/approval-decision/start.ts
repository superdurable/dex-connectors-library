// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type Flow } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newExcelProjectConnection } from '@superdurable/connector-microsoft-excel';
import { AppendRowsFlow } from './append-rows-flow.js';
import { ConnectionName, ReadTableFlow } from './read-table-flow.js';
import { ReadValuesFlow } from './read-values-flow.js';
import { WriteValuesFlow } from './write-values-flow.js';

async function start(): Promise<void> {
  const [operation, inputFile] = process.argv.slice(2);
  if (!operation || !inputFile) throw new TypeError('operation and input JSON file are required');
  const input: unknown = JSON.parse(await readFile(inputFile, 'utf8'));
  const project = await loadFromEnvironment();
  const connection = newExcelProjectConnection(project, ConnectionName);
  const flows = [new ReadTableFlow(connection), new ReadValuesFlow(connection), new WriteValuesFlow(connection), new AppendRowsFlow(connection)];
  const selected: Flow<unknown> | undefined = flows.find(flow => flow.getFlowType().toLowerCase().endsWith(operation.toLowerCase())) as Flow<unknown> | undefined;
  if (!selected) throw new TypeError('operation must be ReadTable, ReadValues, WriteValues, or AppendRows');
  const directory = await mkdtemp(join(tmpdir(), 'dex-excel-typescript-client-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(new Registry(flows), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    const flowId = `excel-typescript-${randomUUID()}`;
    await client.startFlow(selected, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new TypeError('Microsoft Excel Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput())}\n`);
  } finally {
    await client.close(); cache.close(); await rm(directory, { recursive: true, force: true });
  }
}

try { await start(); }
catch { console.error('Microsoft Excel Flow could not start or complete.'); process.exitCode = 1; }
