// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { DocsClient } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { ConnectionName, PolicyDocumentsFlow, type PolicyDocumentRequest } from '../../examples/policy-documents/flow/workflow.js';

it('runs all four operations through a real Dex Worker and rejects an unwired branch', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    requests.push(`${request.method} ${request.url}`);
    response.setHeader('Content-Type', 'application/json');
    if (request.headers.authorization !== 'Bearer docs-test-token') { response.writeHead(401).end('{}'); return; }
    const url = new URL(request.url ?? '', 'http://127.0.0.1');
    if (request.method === 'GET' && url.pathname === '/drive/v3/files') { response.end('{"files":[]}'); return; }
    if (request.method === 'POST' && url.pathname === '/drive/v3/files') {
      response.end('{"id":"newDoc1","name":"Policy","mimeType":"application/vnd.google-apps.document"}'); return;
    }
    if (request.method === 'GET' && url.pathname === '/v1/documents/doc1') {
      response.end(JSON.stringify({ documentId: 'doc1', title: 'Policy', revisionId: 'rev1', tabs: [{ tabProperties: { tabId: 't.0' }, documentTab: { body: { content: [
        { endIndex: 1, sectionBreak: {} }, { startIndex: 1, endIndex: 7, paragraph: { elements: [{ textRun: { content: 'Hello\n' } }] } },
      ] } } }] })); return;
    }
    if (request.method === 'POST' && url.pathname === '/v1/documents/doc1:batchUpdate') {
      response.end('{"documentId":"doc1","replies":[],"writeControl":{"requiredRevisionId":"rev2"}}'); return;
    }
    response.writeHead(404).end('{}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('provider stand-in port is unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-google-docs-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'google', name: ConnectionName };
  const provider = new DocsClient({ docsEndpoint: `http://127.0.0.1:${address.port}`, driveEndpoint: `http://127.0.0.1:${address.port}` },
    new StaticCredentialProvider([[connection, { accessToken: new SecretString('docs-test-token') }]]));
  const flow = new PolicyDocumentsFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const cases: Array<{ input: PolicyDocumentRequest; field: string; value: string }> = [
      { input: { operation: 'getDocumentText', request: { documentId: 'doc1' } }, field: 'text', value: 'Hello' },
      { input: { operation: 'createDocument', request: { title: 'Policy' } }, field: 'document', value: 'newDoc1' },
      { input: { operation: 'replaceDocumentText', request: { documentId: 'doc1', requiredRevisionId: 'rev1', target: 'wholeBody', text: 'New text' } }, field: 'revisionId', value: 'rev2' },
      { input: { operation: 'appendText', request: { documentId: 'doc1', requiredRevisionId: 'rev1', text: 'New text' } }, field: 'revisionId', value: 'rev2' },
    ];
    for (const testCase of cases) {
      const flowId = `google-docs-${randomUUID()}`;
      await client.startFlow(flow, flowId, testCase.input);
      const result = await client.waitForFlow(flowId, 60_000);
      expect(result.status, result.errorMessage).toBe('completed');
      const output = result.singleOutput<Record<string, unknown>>();
      if (testCase.field === 'document') expect((output.document as { documentId: string }).documentId).toBe(testCase.value);
      else expect(output[testCase.field]).toBe(testCase.value);
    }
    const rejectedId = `google-docs-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { operation: 'getDocumentText', request: { documentId: 'missing' } });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('notFound');
    expect(requests.filter(request => request.startsWith('POST'))).toHaveLength(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
