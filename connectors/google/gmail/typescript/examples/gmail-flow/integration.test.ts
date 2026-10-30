// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { afterAll, beforeAll, expect, it } from 'vitest';
import { Client, Registry, Worker, openBlobCache, type BlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GmailClient, GmailConnection } from '@superdurable/connector-google-gmail';
import { GmailMock, getMessageDeletedMessage, getMessageRateLimited } from '@superdurable/connector-google-gmail/mock';
import { ConnectionName, GmailReadFlow } from './read-flow.js';
import { GmailSendFlow } from './send-flow.js';
import { GmailReplyFlow } from './reply-flow.js';

const serviceAddress = process.env.DEX_FLOW_SERVICE_ADDRESS ?? '127.0.0.1:8801';
const mock = new GmailMock(ConnectionName);
const readFlow = new GmailReadFlow(mock.connection());
const sendFlow = new GmailSendFlow(mock.connection());
const replyFlow = new GmailReplyFlow(mock.connection());
const registry = new Registry([readFlow, sendFlow, replyFlow]);
let cacheDirectory: string;
let cache: BlobCache;
let client: Client;
let worker: Worker;

beforeAll(async () => {
  cacheDirectory = await mkdtemp(join(tmpdir(), 'gmail-dex-integration-'));
  cache = openBlobCache({ directory: cacheDirectory, maxBytes: 64 * 1024 * 1024 });
  worker = new Worker(registry, cache, { serverAddress: serviceAddress });
  client = new Client(registry, cache, { serverAddress: serviceAddress });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 15_000 });
}, 30_000);

afterAll(async () => {
  if (client) await client.close();
  if (worker) await worker.close();
  if (cache) cache.close();
  if (cacheDirectory) await rm(cacheDirectory, { recursive: true, force: true });
});

it('runs the checked-in Flow through all three Gmail operation Steps', async () => {
  const readId = `gmail-read-${randomUUID()}`;
  const sendId = `gmail-send-${randomUUID()}`;
  const replyId = `gmail-reply-${randomUUID()}`;
  await client.startFlow(readFlow, readId, { messageId: '18c0a1b2c3d4e5f6' });
  await client.startFlow(sendFlow, sendId, { to: ['ada@example.com'], subject: 'Hello', textBody: 'Update' });
  await client.startFlow(replyFlow, replyId, { messageId: '18c0a1b2c3d4e5f6', textBody: 'Answer' });
  const results = await Promise.all([readId, sendId, replyId].map(id => client.waitForFlow(id, 20_000)));
  expect(results.map(result => result.status)).toEqual(['completed', 'completed', 'completed']);
  expect(results[0]?.singleOutput()).toMatchObject({ branch: 'read', value: { messageId: '18c0a1b2c3d4e5f6' } });
  expect(results[1]?.singleOutput()).toMatchObject({ branch: 'sent', value: { messageId: '18c0a1b2c3d4e600' } });
  expect(results[2]?.singleOutput()).toMatchObject({ branch: 'sent', value: { messageId: '18c0a1b2c3d4e700' } });
  expect(mock.getMessage().calls().some(call => call.flowId === readId)).toBe(true);
  expect(mock.sendMessage().calls().some(call => call.flowId === sendId)).toBe(true);
  expect(mock.replyToMessage().calls().some(call => call.flowId === replyId)).toBe(true);
  mock.getMessage().assertNoFailures();
  mock.sendMessage().assertNoFailures();
  mock.replyToMessage().assertNoFailures();
}, 30_000);

it('retries a safe read in Dex and fails the Flow on an unwired optional branch', async () => {
  const retryId = `gmail-retry-${randomUUID()}`;
  const missingId = `gmail-missing-${randomUUID()}`;
  mock.getMessage().forFlow(retryId).respond(getMessageRateLimited());
  mock.getMessage().forFlow(missingId).respond(getMessageDeletedMessage());
  await client.startFlow(readFlow, retryId, { messageId: '18c0a1b2c3d4e5f6' });
  await client.startFlow(readFlow, missingId, { messageId: 'deleted' });
  const [retried, missing] = await Promise.all([client.waitForFlow(retryId, 20_000), client.waitForFlow(missingId, 20_000)]);
  expect(retried.status).toBe('completed');
  expect(missing.status).toBe('failed');
  expect(mock.getMessage().calls().filter(call => call.flowId === retryId)).toHaveLength(2);
  expect(mock.getMessage().calls().filter(call => call.flowId === missingId)).toHaveLength(1);
  mock.getMessage().assertNoFailures();
}, 30_000);

it('preserves the send claim across a real Dex retry and confirms by Sent Mail', async () => {
  await worker.close();
  let postCount = 0;
  let searchCount = 0;
  const connectionReference = { provider: 'google', name: ConnectionName };
  const credentials = { accessToken: new SecretString('integration-test-token'), primaryEmail: 'owner@example.com' };
  const transport = async (request: ProviderRequest): Promise<ProviderResponse> => {
    if (request.method === 'POST') { postCount++; throw new ProviderTransportError(false); }
    searchCount++;
    const messages = searchCount >= 5 ? [{ id: 'confirmed-send', threadId: 'confirmed-thread' }] : [];
    return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ messages })) };
  };
  const providerClient = new GmailClient({ endpoint: 'http://127.0.0.1:8080' },
    new StaticCredentialProvider([[connectionReference, credentials]]), { transport });
  const providerFlow = new GmailSendFlow(new GmailConnection(providerClient, connectionReference));
  const providerRegistry = new Registry([providerFlow]);
  const providerWorker = new Worker(providerRegistry, cache, { serverAddress: serviceAddress });
  const providerDexClient = new Client(providerRegistry, cache, { serverAddress: serviceAddress });
  try {
    await providerWorker.start();
    await providerWorker.waitPullRegistered({ timeoutMs: 15_000 });
    const flowId = `gmail-fenced-send-${randomUUID()}`;
    await providerDexClient.startFlow(providerFlow, flowId, { to: ['ada@example.com'], subject: 'Hello', textBody: 'Update' });
    const result = await providerDexClient.waitForFlow(flowId, 20_000);
    expect(result.status).toBe('completed');
    expect(result.singleOutput()).toMatchObject({ branch: 'sent', value: { messageId: 'confirmed-send' } });
    expect(postCount).toBe(1);
    expect(searchCount).toBe(5);
  } finally {
    await providerDexClient.close();
    await providerWorker.close();
  }
}, 30_000);

it('ignores a 503 error-body ID and safely resends after a real Dex retry', async () => {
  let postCount = 0;
  let searchCount = 0;
  const connectionReference = { provider: 'google', name: ConnectionName };
  const credentials = { accessToken: new SecretString('integration-test-token'), primaryEmail: 'owner@example.com' };
  const transport = async (request: ProviderRequest): Promise<ProviderResponse> => {
    if (request.method === 'GET') {
      searchCount++;
      return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ messages: [] })) };
    }
    postCount++;
    const body = postCount === 1 ? { id: 'not-a-message' } : { id: 'sent-after-503', threadId: 'thread-1' };
    return { status: postCount === 1 ? 503 : 200, headers: {}, body: Buffer.from(JSON.stringify(body)) };
  };
  const providerClient = new GmailClient({ endpoint: 'http://127.0.0.1:8080' },
    new StaticCredentialProvider([[connectionReference, credentials]]), { transport });
  const providerFlow = new GmailSendFlow(new GmailConnection(providerClient, connectionReference));
  const providerRegistry = new Registry([providerFlow]);
  const providerWorker = new Worker(providerRegistry, cache, { serverAddress: serviceAddress });
  const providerDexClient = new Client(providerRegistry, cache, { serverAddress: serviceAddress });
  try {
    await providerWorker.start();
    await providerWorker.waitPullRegistered({ timeoutMs: 15_000 });
    const flowId = `gmail-refused-send-${randomUUID()}`;
    await providerDexClient.startFlow(providerFlow, flowId, { to: ['ada@example.com'], subject: 'Hello', textBody: 'Update' });
    const result = await providerDexClient.waitForFlow(flowId, 20_000);
    expect(result.status).toBe('completed');
    expect(result.singleOutput()).toMatchObject({ branch: 'sent', value: { messageId: 'sent-after-503' } });
    expect(postCount).toBe(2);
    expect(searchCount).toBe(5);
  } finally {
    await providerDexClient.close();
    await providerWorker.close();
  }
}, 30_000);
