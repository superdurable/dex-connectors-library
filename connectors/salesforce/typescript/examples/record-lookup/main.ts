// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-salesforce';
import { ConnectionName, SalesforceRecordLookupFlow } from './flow.js';

/** Serve the checked-in Salesforce Flow on a real Dex Worker. */
export async function runSalesforceWorker(): Promise<void> {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  const registry = new Registry([new SalesforceRecordLookupFlow(connection)]);
  const directory = await mkdtemp(join(tmpdir(), 'salesforce-example-cache-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  try {
    const worker = new Worker(registry, cache, {
      serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS ?? '127.0.0.1:8801',
    });
    const stop = () => { void worker.close().catch(() => { process.exitCode = 1; }); };
    process.once('SIGINT', stop);
    process.once('SIGTERM', stop);
    try { await worker.run(); }
    finally {
      process.off('SIGINT', stop);
      process.off('SIGTERM', stop);
      await worker.close();
    }
  } finally {
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  runSalesforceWorker().catch(() => { process.stderr.write('Salesforce example Worker failed\n'); process.exitCode = 1; });
}
