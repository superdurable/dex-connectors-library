// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { PostgreSQLClient, newConnection, type Config } from '@superdurable/connector-postgresql';
import { ConnectionName, RecordRefundFlow } from './flow/workflow.js';

/** Construct a Worker-only connection using a private environment password. */
export function createRefundFlow(): RecordRefundFlow {
  const configuration: Config = {
    host: process.env.POSTGRESQL_CONNECTOR_TEST_HOST ?? '',
    port: Number(process.env.POSTGRESQL_CONNECTOR_TEST_PORT ?? '5432'),
    database: process.env.POSTGRESQL_CONNECTOR_TEST_DATABASE ?? '',
    user: process.env.POSTGRESQL_CONNECTOR_TEST_USER ?? '',
    sslMode: process.env.POSTGRESQL_CONNECTOR_TEST_SSL_MODE ?? 'require',
  };
  const provider = new PostgreSQLClient(configuration, {
    resolve: async () => ({ password: new SecretString(process.env.POSTGRESQL_CONNECTOR_TEST_PASSWORD ?? '') }),
  });
  return new RecordRefundFlow(newConnection(provider, { provider: 'postgresql', name: ConnectionName }));
}

const stop = new AbortController();
const stopWorker = (): void => stop.abort();
process.once('SIGINT', stopWorker);
process.once('SIGTERM', stopWorker);
const directory = await mkdtemp(join(tmpdir(), 'dex-postgresql-refund-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
const worker = new Worker(new Registry([createRefundFlow()]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
  await new Promise<void>(resolve => { if (stop.signal.aborted) resolve(); else stop.signal.addEventListener('abort', () => resolve(), { once: true }); });
} catch {
  console.error('PostgreSQL refund Worker could not start or stopped with an error.');
  process.exitCode = 1;
} finally {
  await worker.close(); cache.close();
  await rm(directory, { recursive: true, force: true });
  process.off('SIGINT', stopWorker); process.off('SIGTERM', stopWorker);
}
