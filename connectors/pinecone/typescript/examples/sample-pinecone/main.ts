// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-pinecone';
import { ConnectionName, PineconeSampleFlow } from './flow/workflow.js';
import { runPineconeWorker } from './worker-runtime.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
try {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  await runPineconeWorker(new PineconeSampleFlow(connection), stop.signal);
} catch {
  console.error('Pinecone sample Worker could not start or stopped with an error.');
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
