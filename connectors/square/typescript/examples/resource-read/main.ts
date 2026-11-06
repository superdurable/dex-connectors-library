// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-square';
import { ConnectionName, ResourceReadFlow } from './flow/workflow.js';
import { runResourceReadWorker } from './worker-runtime.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);

try {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  await runResourceReadWorker(new ResourceReadFlow(connection), stop.signal);
} catch {
  console.error('Square read Worker could not start or stopped with an error.');
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
