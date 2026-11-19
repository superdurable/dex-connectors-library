// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { SQLServerClient } from '../src/client.js';
import { newConnection } from '../src/generated/connection.js';
import { GeneratedMock } from '../src/generated/mock.js';
import { ConnectionName, QueryRowsFlow } from '../examples/query-rows/flow/workflow.js';
import { ConnectionName as RefundConnectionName, RecordRefundFlow } from '../examples/record-refund/flow/workflow.js';

it('runs the checked-in query Flow through a real Dex Worker and SQL Server', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  const host = process.env.SQLSERVER_CONNECTOR_TEST_HOST;
  const password = process.env.SQLSERVER_CONNECTOR_TEST_PASSWORD;
  if (!dexAddress || !host || !password) throw new Error('set DEX_FLOW_SERVICE_ADDRESS and SQLSERVER_CONNECTOR_TEST_HOST/PASSWORD');
  const provider = new SQLServerClient({ host, port: Number(process.env.SQLSERVER_CONNECTOR_TEST_PORT), database: process.env.SQLSERVER_CONNECTOR_TEST_DATABASE || 'master', user: process.env.SQLSERVER_CONNECTOR_TEST_USER || 'sa', encrypt: process.env.SQLSERVER_CONNECTOR_TEST_ENCRYPT || 'mandatory' }, { resolve: async () => ({ password: new SecretString(password) }) });
  const flow = new QueryRowsFlow(newConnection(provider, { provider: 'microsoft-sql-server', name: ConnectionName }));
  const directory = await mkdtemp(join(tmpdir(), 'dex-sql-server-query-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `sql-server-query-${randomUUID()}`;
    await client.startFlow(flow, flowId, { statement: 'SELECT CAST(@p1 AS bigint) AS refund_id', parameters: ['88213'] });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<{ rows: Array<{ refund_id: string }> }>().rows[0]?.refund_id).toBe('88213');
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 90_000);

it('runs the record-refund Flow with its daily total through a real Dex Worker and the generated mock', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS');
  const mock = new GeneratedMock(RefundConnectionName);
  const flow = new RecordRefundFlow(mock.connection());
  const directory = await mkdtemp(join(tmpdir(), 'dex-sql-server-refund-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `sql-server-refund-${randomUUID()}`;
    await client.startFlow(flow, flowId, { orderId: '88213', amountUsd: '250.00', externalReference: 'tkt_5488' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const dailyTotalCalls = mock.executeNonIdempotent().calls();
    expect(dailyTotalCalls).toHaveLength(1);
    expect(dailyTotalCalls[0]?.input).toMatchObject({ parameters: ['250.00'], maxRowsAffected: 1 });
    mock.assertNoFailures();
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 90_000);
