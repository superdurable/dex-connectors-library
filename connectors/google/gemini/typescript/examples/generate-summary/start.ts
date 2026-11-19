// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, type GenerateContentResponse } from '@superdurable/connector-google-gemini';
import { ConnectionName, GenerateSummaryFlow } from './flow/workflow.js';

const text = process.argv.slice(2).join(' ');
if (!text) throw new TypeError('Summary text is required');
const project = await loadFromEnvironment();
const flow = new GenerateSummaryFlow(newProjectConnection(project, ConnectionName));
const directory = await mkdtemp(join(tmpdir(), 'dex-gemini-client-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
const client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `gemini-summary-${randomUUID()}`;
  await client.startFlow(flow, flowId, text);
  const result = await client.waitForFlow(flowId, 90_000);
  if (result.status !== 'completed') throw new Error('Gemini summary Flow did not complete');
  process.stdout.write(`${JSON.stringify(result.singleOutput<GenerateContentResponse>())}\n`);
} finally { await client.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
