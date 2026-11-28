// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { ConnectionName, ClusterListFlow } from '../../examples/cluster-list/flow.js';
import { ZillizClient } from '../../src/client.js';
import { newConnection, type ListClustersOutput } from '../../src/index.js';

it('retries Zilliz 503 through a real Dex Worker and persists the cluster list', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'GET' || request.url !== '/v2/clusters?pageSize=25&currentPage=1' || request.headers.authorization !== 'Bearer test-secret' || request.headers.accept !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: 0, data: { count: 1, clusters: [{ clusterId: 'inxx-example', clusterName: 'example-cluster', regionId: null, status: 'RUNNING' }] } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Zilliz fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-zilliz-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ZillizClient({}, { resolve: async () => ({ apiKey: new SecretString('test-secret') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/v2/clusters?pageSize=25&currentPage=1`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new ClusterListFlow(newConnection(provider, { provider: 'zilliz', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `zilliz-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ListClustersOutput>()).toEqual({ total: 1, clusters: [{ clusterID: 'inxx-example', name: 'example-cluster', regionID: '', plan: '', status: 'RUNNING' }] });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
