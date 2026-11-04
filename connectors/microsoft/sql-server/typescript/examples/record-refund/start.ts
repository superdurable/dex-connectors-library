// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type BlobCache } from '@superdurable/dex';
import { z } from 'zod';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, type StatementExecution } from '@superdurable/connector-microsoft-sql-server';
import { ConnectionName, RecordRefundFlow } from './flow/workflow.js';

const RefundInputSchema = z.object({ orderId: z.string().min(1).max(100), amountUsd: z.string().regex(/^\d{1,10}\.\d{2}$/).refine(value => Number(value) > 0), externalReference: z.string().min(1).max(200) });

async function startRecordRefund(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('SQL Server refund input JSON file is required');
  const input = RefundInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')));
  const project = await loadFromEnvironment();
  const flow = new RecordRefundFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-sql-server-refund-client-'));
  let cache: BlobCache | undefined;
  let client: Client | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
    const flowId = `sql-server-refund-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('SQL Server refund Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<StatementExecution>())}\n`);
  } finally {
    try { await client?.close(); } finally {
      cache?.close();
      await rm(directory, { recursive: true, force: true });
    }
  }
}

try { await startRecordRefund(); } catch {
  console.error('SQL Server refund Flow could not start or complete.');
  process.exitCode = 1;
}
