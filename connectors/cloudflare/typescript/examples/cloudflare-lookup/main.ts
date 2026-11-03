// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-cloudflare';
import { ConnectionName, CloudflareLookupFlow } from './flow/workflow.js';
import { runCloudflareLookupWorker } from './worker-runtime.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
try {
  const project = await loadFromEnvironment();
  await runCloudflareLookupWorker(new CloudflareLookupFlow(newProjectConnection(project, ConnectionName)), stop.signal);
} catch {
  console.error('Cloudflare lookup Worker could not start or stopped with an error.');
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
