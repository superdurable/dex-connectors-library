// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { GenerateTextRequestSchema, newProjectConnection, type GenerateTextResponse } from '../../src/index.js';
import { ConnectionName, SummarizeTextFlow } from './flow.js';

const inputFile = process.argv[2];
if (!inputFile) throw new TypeError('summarize-text request JSON file is required');
const input = GenerateTextRequestSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')) as unknown);
const project = await loadFromEnvironment();
const flow = new SummarizeTextFlow(newProjectConnection(project, ConnectionName));
const directory = await mkdtemp(join(tmpdir(), 'dex-llm-client-'));
const blobCache = openBlobCache({ directory, maxBytes: 64 << 20 });
const client = new DexClient(new Registry([flow]), blobCache, {
  serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801',
});
try {
  const flowID = 'llm-summary-' + randomUUID();
  await client.startFlow(flow, flowID, input);
  const result = await client.waitForFlow(flowID, 120_000);
  if (result.status !== 'completed') throw new Error('LLM summarize-text Flow did not complete');
  process.stdout.write(JSON.stringify(result.singleOutput<GenerateTextResponse>()) + '\n');
} finally {
  await client.close();
  blobCache.close();
  await rm(directory, { recursive: true, force: true });
}
