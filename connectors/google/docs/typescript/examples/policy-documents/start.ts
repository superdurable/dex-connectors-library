// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type BlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { AppendTextInputSchema, CreateDocumentInputSchema, GetDocumentTextInputSchema, ReplaceDocumentTextInputSchema, newProjectConnection } from '@superdurable/connector-google-docs';
import { ConnectionName, PolicyDocumentsFlow, type PolicyDocumentRequest } from './flow/workflow.js';

async function startPolicyDocuments(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('Google Docs input JSON file is required');
  const input = parsePolicyDocumentRequest(JSON.parse(await readFile(inputFile, 'utf8')) as unknown);
  const project = await loadFromEnvironment();
  const flow = new PolicyDocumentsFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-google-docs-client-'));
  let cache: BlobCache | undefined;
  let client: Client | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
    const flowId = `google-docs-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('Google Docs Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<unknown>())}\n`);
  } finally {
    try { await client?.close(); } finally {
      cache?.close();
      await rm(directory, { recursive: true, force: true });
    }
  }
}

function parsePolicyDocumentRequest(value: unknown): PolicyDocumentRequest {
  if (!value || typeof value !== 'object' || !('operation' in value) || !('request' in value)) throw new TypeError('operation and request are required');
  switch (value.operation) {
    case 'getDocumentText': return { operation: 'getDocumentText', request: GetDocumentTextInputSchema.parse(value.request) };
    case 'createDocument': return { operation: 'createDocument', request: CreateDocumentInputSchema.parse(value.request) };
    case 'replaceDocumentText': return { operation: 'replaceDocumentText', request: ReplaceDocumentTextInputSchema.parse(value.request) };
    case 'appendText': return { operation: 'appendText', request: AppendTextInputSchema.parse(value.request) };
    default: throw new TypeError('unsupported Google Docs operation');
  }
}

try { await startPolicyDocuments(); } catch {
  console.error('Google Docs Flow could not start or complete.');
  process.exitCode = 1;
}
