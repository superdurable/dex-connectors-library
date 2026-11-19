// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, it } from 'vitest';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { HuaweiCloudClient, newConnection, type ListAccessibleProjectsOutput } from '../../src/index.js';
import { HuaweiCloudProjectListFlow, ConnectionName, projectListAttribute } from '../../examples/project-list/flow.js';

const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
const encoded = (value: unknown) => new TextEncoder().encode(JSON.stringify(value));
let tokenRequests = 0;
let projectRequests = 0;
let shouldReject = false;
async function send(request: ProviderRequest): Promise<ProviderResponse> {
  if (request.url.endsWith('/v3/auth/tokens')) {
    tokenRequests++;
    if (shouldReject) return { status: 401, headers: {}, body: encoded({}) };
    return { status: 201, headers: { 'X-Subject-Token': 'synthetic-test-token' }, body: encoded({}) };
  }
  if (request.url.endsWith('/v3/auth/projects') && request.headers?.['X-Auth-Token'] === 'synthetic-test-token') {
    projectRequests++;
    if (projectRequests === 1) return { status: 503, headers: {}, body: encoded({}) };
    return { status: 200, headers: {}, body: encoded({ projects: [
      { id: 'project-123', name: 'Example Project', enabled: true, domain_id: 'domain-123' },
    ] }) };
  }
  return { status: 400, headers: {}, body: encoded({}) };
}

it('retries the checked-in Flow, persists IAM projects, and fails on an unwired rejection', async () => {
  const reference = { provider: 'huaweicloud', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[reference, {
    accountName: new SecretString('example-account'), iamUsername: new SecretString('example-user'), iamPassword: new SecretString('synthetic-test-password'),
  }]]);
  const connector = new HuaweiCloudClient({}, credentials, { send });
  const flow = new HuaweiCloudProjectListFlow(newConnection(connector, reference));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-huaweicloud-ts-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = 'huaweicloud-ts-' + randomUUID();
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const output: ListAccessibleProjectsOutput = { projects: [
      { id: 'project-123', name: 'Example Project', enabled: true, domainId: 'domain-123' },
    ] };
    expect(result.singleOutput<ListAccessibleProjectsOutput>()).toEqual(output);
    expect(await client.getAttributes(flowId, projectListAttribute)).toEqual([output]);
    expect(tokenRequests).toBe(2);
    expect(projectRequests).toBe(2);
    shouldReject = true;
    const rejectedId = 'huaweicloud-ts-rejected-' + randomUUID();
    await client.startFlow(flow, rejectedId, {});
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toMatch(/providerRejected/);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
