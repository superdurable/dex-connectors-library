// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newExcelProjectConnection } from '@superdurable/connector-microsoft-excel';
import { AppendRowsFlow } from './append-rows-flow.js';
import { ConnectionName, ReadTableFlow } from './read-table-flow.js';
import { ReadValuesFlow } from './read-values-flow.js';
import { WriteValuesFlow } from './write-values-flow.js';

const stop = new AbortController();
const interrupt = (): void => stop.abort();
process.once('SIGINT', interrupt); process.once('SIGTERM', interrupt);

async function runWorker(): Promise<void> {
  const project = await loadFromEnvironment();
  const connection = newExcelProjectConnection(project, ConnectionName);
  const flows = [new ReadTableFlow(connection), new ReadValuesFlow(connection), new WriteValuesFlow(connection), new AppendRowsFlow(connection)];
  const directory = await mkdtemp(join(tmpdir(), 'dex-excel-typescript-worker-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(new Registry(flows), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    await new Promise<void>(resolve => {
      if (stop.signal.aborted) { resolve(); return; }
      stop.signal.addEventListener('abort', () => resolve(), { once: true });
    });
  } finally {
    await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true });
  }
}

try { await runWorker(); }
catch { console.error('Microsoft Excel Worker could not start or stopped with an error.'); process.exitCode = 1; }
finally { process.off('SIGINT', interrupt); process.off('SIGTERM', interrupt); }
