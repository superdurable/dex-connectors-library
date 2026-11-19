// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, ListHiverInboxesFlow } from './flow.js';
import { runListHiverInboxesWorker } from './worker.js';

const stop = new AbortController();
process.once('SIGINT', () => stop.abort());
process.once('SIGTERM', () => stop.abort());
try {
  const project = await loadFromEnvironment();
  await runListHiverInboxesWorker(new ListHiverInboxesFlow(newProjectConnection(project, ConnectionName)), stop.signal,
    process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801');
} catch {
  console.error('Hiver inbox Worker could not start or stopped with an error.');
  process.exitCode = 1;
}
