// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-brevo';
import { ConnectionName, ContactSyncFlow } from './flow/workflow.js';
import { runContactSyncWorker } from './worker-runtime.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);

try {
  const project = await loadFromEnvironment();
  await runContactSyncWorker(new ContactSyncFlow(newProjectConnection(project, ConnectionName)), stop.signal);
} catch {
  console.error('Brevo contact Worker could not start or stopped with an error.');
  process.exitCode = 1;
}
