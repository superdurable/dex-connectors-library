// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, GmailExampleFlow } from './flow.js';

/** Start one real Dex Worker with the Gmail connection named in the example Flow. */
export async function runGmailExampleWorker(): Promise<void> {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  const flow = new GmailExampleFlow(connection);
  const registry = new Registry([flow]);
  const cacheDirectory = await mkdtemp(join(tmpdir(), 'gmail-example-cache-'));
  const cache = openBlobCache({ directory: cacheDirectory, maxBytes: 64 * 1024 * 1024 });
  try {
    const worker = new Worker(registry, cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS ?? '127.0.0.1:8801' });
    const stopWorker = () => { void worker.close().catch(() => { process.exitCode = 1; }); };
    process.once('SIGINT', stopWorker);
    process.once('SIGTERM', stopWorker);
    try { await worker.run(); }
    finally {
      process.off('SIGINT', stopWorker);
      process.off('SIGTERM', stopWorker);
      await worker.close();
    }
  } finally {
    cache.close();
    await rm(cacheDirectory, { recursive: true, force: true });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  runGmailExampleWorker().catch(() => { process.stderr.write('Gmail example Worker failed\n'); process.exitCode = 1; });
}
