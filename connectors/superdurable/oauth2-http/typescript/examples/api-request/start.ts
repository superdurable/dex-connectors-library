// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type BlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, type SendRequestOutput } from '@superdurable/connector-oauth2-http';
import { APIRequestFlow, ConnectionName, type RequestInput } from './flow/workflow.js';

async function startRequest(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('OAuth 2.0 HTTP input JSON file is required');
  const input = JSON.parse(await readFile(inputFile, 'utf8')) as RequestInput;
  if (typeof input.path !== 'string') throw new TypeError('path must be a string');
  const project = await loadFromEnvironment();
  const flow = new APIRequestFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-oauth2-http-client-'));
  let cache: BlobCache | undefined;
  let client: Client | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
    const flowId = `oauth2-http-request-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('OAuth 2.0 HTTP Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<SendRequestOutput>())}\n`);
  } finally {
    try { await client?.close(); } finally {
      cache?.close();
      await rm(directory, { recursive: true, force: true });
    }
  }
}

try { await startRequest(); } catch {
  console.error('OAuth 2.0 HTTP Flow could not start or complete.');
  process.exitCode = 1;
}
