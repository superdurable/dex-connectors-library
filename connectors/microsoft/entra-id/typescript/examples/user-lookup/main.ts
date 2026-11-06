// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, UserLookupFlow } from './flow.js';

/** Start the checked-in Flow on one Dex Worker. */
export async function runEntraExampleWorker(): Promise<void> {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  const registry = new Registry([new UserLookupFlow(connection)]);
  const directory = await mkdtemp(join(tmpdir(), 'entra-user-lookup-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  try {
    const worker = new Worker(registry, cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS ?? '127.0.0.1:8801' });
    try { await worker.run(); } finally { await worker.close(); }
  } finally { cache.close(); await rm(directory, { recursive: true, force: true }); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  runEntraExampleWorker().catch(() => { process.stderr.write('Entra example Worker failed\n'); process.exitCode = 1; });
}
