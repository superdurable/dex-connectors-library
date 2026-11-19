// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { ConnectionName, SummarizeTextFlow } from './flow.js';
import { runWorker } from './worker-runtime.js';

const stop = new AbortController();
const handleSignal = () => stop.abort();
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
try {
  const project = await loadFromEnvironment();
  await runWorker(
    new SummarizeTextFlow(newProjectConnection(project, ConnectionName)),
    stop.signal, process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801',
  );
} catch {
  console.error('LLM summarize-text Worker could not start or stopped with an error.');
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
