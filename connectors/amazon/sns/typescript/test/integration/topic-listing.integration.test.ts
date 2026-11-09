// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { AmazonSNSClient } from '../../src/client.js';
import { newConnection, type TopicPage, type Credentials } from '../../src/index.js';
import { ConnectionName, TopicListingFlow } from '../../examples/topic-listing/flow.js';

it('retries a provider 503 and persists the topic page through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const nextToken = '';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (!request.headers.authorization?.includes('/eu-west-1/sns/aws4_request')) { response.writeHead(401).end(); return; }
    if (request.method !== 'GET' || request.url !== '/?Action=ListTopics&Version=2010-03-31') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/xml' }).end('<ListTopicsResponse xmlns="https://sns.amazonaws.com/doc/2010-03-31/"><ListTopicsResult><Topics><member><TopicArn>arn:aws:sns:eu-west-1:123456789012:alerts</TopicArn></member></Topics></ListTopicsResult></ListTopicsResponse>');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Amazon SNS fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-sns-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { accessKeyId: new SecretString('AKIASNSCONNECTOR'), secretAccessKey: new SecretString('sns-test-secret'), sessionToken: new SecretString('') };
  const provider = new AmazonSNSClient({ endpoint: `http://127.0.0.1:${address.port}`, region: 'eu-west-1' }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'amazon-sns', name: ConnectionName });
  const flow = new TopicListingFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `sns-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { nextToken });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<TopicPage>().topicARNs?.[0]).toBe('arn:aws:sns:eu-west-1:123456789012:alerts');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
