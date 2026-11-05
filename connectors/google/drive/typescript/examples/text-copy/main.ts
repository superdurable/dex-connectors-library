// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-google-drive';
import { ConnectionName, TextCopyFlow } from './flow.js';

/** Run the checked-in Drive Flow with its named project connection. */
export async function runTextCopyWorker(): Promise<void> {
  const project = await loadFromEnvironment();
  const flow = new TextCopyFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-drive-copy-worker-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  const stop = () => { void worker.close(); };
  process.once('SIGINT', stop); process.once('SIGTERM', stop);
  try { await worker.run(); }
  finally { process.off('SIGINT', stop); process.off('SIGTERM', stop); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  runTextCopyWorker().catch(() => { process.stderr.write('Google Drive text copy Worker failed\n'); process.exitCode = 1; });
}
