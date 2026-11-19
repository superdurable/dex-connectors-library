// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { CainiaoWaybillTraceFlow, ConnectionName } from './flow.js';
import { runWaybillTraceWorker } from './worker.js';

const shutdown = new AbortController();
process.once('SIGINT', () => shutdown.abort());
process.once('SIGTERM', () => shutdown.abort());
try {
  const project = await loadFromEnvironment();
  await runWaybillTraceWorker(new CainiaoWaybillTraceFlow(newProjectConnection(project, ConnectionName)), shutdown.signal,
    process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-cainiao-waybill-trace-blobs'));
} catch (error) {
  console.error('Cainiao waybill trace Worker stopped', error instanceof Error ? error.name : 'unknown error');
  process.exitCode = 1;
}
