// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type BlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-amazon-s3';
import { ConnectionName, ReportArchiveFlow, type ReportArchiveInput, type ReportArchiveOutput } from './flow/workflow.js';

async function startReportArchive(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('report archive input JSON file is required');
  const input = JSON.parse(await readFile(inputFile, 'utf8')) as ReportArchiveInput;
  if (!input.bucket || !input.key || !input.text) throw new TypeError('bucket, key, and text are required');
  const project = await loadFromEnvironment();
  const flow = new ReportArchiveFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-s3-report-client-'));
  let cache: BlobCache | undefined;
  let client: Client | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
    const flowId = `s3-report-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('report archive Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<ReportArchiveOutput>())}\n`);
  } finally {
    try { await client?.close(); } finally { cache?.close(); await rm(directory, { recursive: true, force: true }); }
  }
}

try { await startReportArchive(); } catch {
  console.error('Amazon S3 report archive Flow could not start or complete.');
  process.exitCode = 1;
}
