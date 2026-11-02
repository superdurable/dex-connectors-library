// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { S3Client } from '../../src/client.js';
import { newConnection } from '../../src/connection.js';
import { ReportArchiveFlow, ConnectionName, type ReportArchiveOutput } from '../../examples/report-archive/flow/workflow.js';

it('runs the checked-in Flow through real Dex, retries a lost create-only reply, and fails an unwired rejection', async () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const objectBytes = new Map<string, Buffer>();
  const objectMarkers = new Map<string, string>();
  const requestKinds: string[] = [];
  let dropFirstPutResponse = true;
  const server = createServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const body = Buffer.concat(chunks);
    const url = new URL(request.url ?? '', 'http://localhost');
    const isListing = url.searchParams.get('list-type') === '2';
    const key = isListing ? url.searchParams.get('prefix') ?? '' : decodeURIComponent(url.pathname.replace(/^\/acme-reports\//, ''));
    requestKinds.push(`${request.method} ${isListing ? 'list' : key}`);
    if (!request.headers.authorization?.startsWith('AWS4-HMAC-SHA256 Credential=AKIAS3CONNECTORTEST1/')) { response.writeHead(403).end(); return; }
    if (request.headers['x-amz-content-sha256'] !== createHash('sha256').update(body).digest('hex')) { response.writeHead(400).end(); return; }
    if (key.startsWith('denied/')) { response.writeHead(403).end('<Error><Code>AccessDenied</Code></Error>'); return; }
    if (request.method === 'PUT') {
      if (objectBytes.has(key) && request.headers['if-none-match'] === '*') { response.writeHead(412).end(); return; }
      objectBytes.set(key, body);
      objectMarkers.set(key, String(request.headers['x-amz-meta-dex-idempotency-key'] ?? ''));
      if (dropFirstPutResponse) { dropFirstPutResponse = false; response.destroy(); return; }
      response.writeHead(200, { ETag: '"saved"' }).end();
      return;
    }
    if (request.method === 'HEAD' || (request.method === 'GET' && !isListing)) {
      const stored = objectBytes.get(key);
      if (!stored) { response.writeHead(404).end(); return; }
      response.writeHead(200, { 'Content-Length': String(stored.length), 'Last-Modified': 'Sun, 25 Oct 2026 09:00:00 GMT', 'Content-Type': 'text/plain; charset=utf-8', ETag: '"saved"', 'X-Amz-Meta-Dex-Idempotency-Key': objectMarkers.get(key) ?? '' });
      response.end(request.method === 'HEAD' ? undefined : stored);
      return;
    }
    if (request.method === 'GET' && isListing) {
      const stored = objectBytes.get(key);
      const xml = stored ? `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>${key}</Key><Size>${stored.length}</Size></Contents></ListBucketResult>` : '<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>';
      response.writeHead(200, { 'Content-Type': 'application/xml' }).end(xml);
      return;
    }
    response.writeHead(400).end();
  });
  server.listen(21290, '127.0.0.1');
  await once(server, 'listening');

  const directory = await mkdtemp(join(tmpdir(), 'dex-s3-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new S3Client({ endpoint: 'http://127.0.0.1:21290', region: 'eu-west-1', defaultBucket: 'acme-reports' }, {
    resolve: async () => ({ accessKeyId: new SecretString('AKIAS3CONNECTORTEST1'), secretAccessKey: new SecretString('s3-connector-test/secret+value') }),
  });
  const flow = new ReportArchiveFlow(newConnection(provider, { provider: 'amazon-s3', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: address });
  const client = new Client(registry, cache, { serverAddress: address });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successfulFlowId = `s3-archive-retry-${randomUUID()}`;
    await client.startFlow(flow, successfulFlowId, { bucket: 'acme-reports', key: 'reports/weekly.md', text: 'hello\n' });
    const successful = await client.waitForFlow(successfulFlowId, 90_000);
    expect(successful.status, successful.errorMessage).toBe('completed');
    const output = successful.singleOutput<ReportArchiveOutput>();
    expect(output.stored.isFromEarlierAttempt).toBe(true);
    expect(output.metadata.sizeBytes).toBe(6);
    expect(output.text.text).toBe('hello\n');
    expect(output.listing.objects?.[0]?.key).toBe('reports/weekly.md');
    expect(requestKinds).toEqual(['PUT reports/weekly.md', 'PUT reports/weekly.md', 'HEAD reports/weekly.md', 'HEAD reports/weekly.md', 'GET reports/weekly.md', 'GET list']);

    const deniedFlowId = `s3-archive-denied-${randomUUID()}`;
    await client.startFlow(flow, deniedFlowId, { bucket: 'acme-reports', key: 'denied/report.md', text: 'blocked' });
    const denied = await client.waitForFlow(deniedFlowId, 30_000);
    expect(denied.status).toBe('failed');
    expect(denied.errorMessage).toContain('providerRejected');
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
