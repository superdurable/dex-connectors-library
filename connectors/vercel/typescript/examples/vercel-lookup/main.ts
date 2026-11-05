// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-vercel';
import { ConnectionName, VercelLookupFlow } from './flow/workflow.js';
import { runVercelWorker } from './worker-runtime.js';

const shutdown = new AbortController();
process.once('SIGINT', () => shutdown.abort());
process.once('SIGTERM', () => shutdown.abort());
try {
  const project = await loadFromEnvironment();
  await runVercelWorker(new VercelLookupFlow(newProjectConnection(project, ConnectionName)), shutdown.signal);
} catch {
  console.error('Vercel lookup Worker could not start or stopped with an error.');
  process.exitCode = 1;
}
