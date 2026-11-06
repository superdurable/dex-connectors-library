// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, WelcomeEmailFlow } from './flow.js';

const project = await loadFromEnvironment();
const flow = new WelcomeEmailFlow(newProjectConnection(project, ConnectionName));
const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-resend-welcome'), maxBytes: 1 << 30 });
const worker = new Worker(new Registry([flow]), blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 10_000 });
  await new Promise<void>(resolve => {
    process.once('SIGINT', resolve);
    process.once('SIGTERM', resolve);
  });
} finally {
  await worker.close();
  blobCache.close();
}
