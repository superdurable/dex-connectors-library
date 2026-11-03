// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, IndexSearchFlow, ReplaceObjectFlow } from './flow.js';
import { runIndexSearchWorker } from './worker.js';

const shutdown = new AbortController();
process.once('SIGINT', () => shutdown.abort());
process.once('SIGTERM', () => shutdown.abort());

try {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  await runIndexSearchWorker([new IndexSearchFlow(connection), new ReplaceObjectFlow(connection)], shutdown.signal,
    process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801',
    process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-algolia-index-search-blobs'));
} catch (error) {
  console.error('Algolia index-search Worker stopped', error instanceof Error ? error.name : 'unknown error');
  process.exitCode = 1;
}
