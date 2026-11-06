// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache, type Flow } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ExcelClient, newConnection, type Credentials } from '@superdurable/connector-microsoft-excel';
import { expect, it } from 'vitest';
import { AppendRowsFlow } from './append-rows-flow.js';
import { ConnectionName, ReadTableFlow } from './read-table-flow.js';
import { ReadValuesFlow } from './read-values-flow.js';
import { WriteValuesFlow } from './write-values-flow.js';

it('runs all four Excel operations through a real Dex Worker and Client', async () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new TypeError('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const reference = { provider: 'microsoft', name: ConnectionName };
  const credentials: Credentials = { authMethodId: 'microsoft-oauth', accessToken: new SecretString('local-excel-token'),
    oauthClientId: '', oauthClientSecret: new SecretString(''), refreshToken: new SecretString('') };
  const requests: string[] = [];
  const transport = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    requests.push(`${request.method} ${url.pathname}`);
    const body = request.method === 'PATCH' ? { address: 'Decisions!A2:B2' }
      : url.pathname.endsWith('/columns') ? { value: [{ id: 'c1', index: 0, name: 'RequestId' }, { id: 'c2', index: 1, name: 'Amount' }] }
      : url.pathname.endsWith('/columns/c1/dataBodyRange') ? { address: 'Decisions!A2:A2', rowCount: 0, columnCount: 1, values: [] }
      : url.pathname.endsWith('/dataBodyRange') ? { address: 'Decisions!A2:B2', rowCount: 1, columnCount: 2, values: [['REQ-1001', 420]] }
      : url.pathname.includes('/range(') ? { address: 'Decisions!A2:B2', rowCount: 1, columnCount: 2, values: [['REQ-1001', 420]], text: [['REQ-1001', '420']] }
      : {};
    return { status: 200, headers: {}, body: Buffer.from(JSON.stringify(body)) };
  };
  const operations = new ExcelClient({}, new StaticCredentialProvider([[reference, credentials]]),
    { endpoint: 'http://127.0.0.1:40661/v1.0', transport, refreshDriver: { refreshRequired: () => false,
      refresh: async () => { throw new Error('unexpected refresh'); } } });
  const connection = newConnection(operations, reference);
  const readTableFlow = new ReadTableFlow(connection);
  const readValuesFlow = new ReadValuesFlow(connection);
  const writeValuesFlow = new WriteValuesFlow(connection);
  const appendRowsFlow = new AppendRowsFlow(connection);
  const flows = [readTableFlow, readValuesFlow, writeValuesFlow, appendRowsFlow];
  const registry = new Registry(flows);
  const directory = await mkdtemp(join(tmpdir(), 'dex-excel-typescript-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: address });
  const client = new Client(registry, cache, { serverAddress: address });
  const workbook = { driveId: 'b!driveKey-123_abc', workbookId: '01BYE5RZ6QN3ZWBTUFOFD3GSPGOHDJD36K' };
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    async function runFlow<Input>(flow: Flow<Input>, input: Input): Promise<void> {
      const flowId = `excel-typescript-${randomUUID()}`;
      await client.startFlow(flow, flowId, input);
      const result = await client.waitForFlow(flowId, 30_000);
      expect(result.status, result.errorMessage).toBe('completed');
    }
    await runFlow(readTableFlow, { ...workbook, table: 'Decisions' });
    await runFlow(readValuesFlow, { ...workbook, worksheet: 'Decisions', address: 'A2:B2' });
    await runFlow(writeValuesFlow, { ...workbook, worksheet: 'Decisions', address: 'A2:B2', values: [['REQ-1001', 420]] });
    await runFlow(appendRowsFlow, { ...workbook, table: 'Decisions', keyColumn: 'RequestId', rows: [{ RequestId: 'REQ-1001', Amount: 420 }] });
    expect(requests.filter(request => request.startsWith('PATCH'))).toHaveLength(1);
    expect(requests.filter(request => request.startsWith('POST'))).toHaveLength(1);
    expect(requests).toHaveLength(7);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
