// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-atlassian-jira-service-management';
import { ConnectionName, FindCustomerFlow } from './flow/workflow.js';
import { runFindCustomerWorker } from './worker-runtime.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
try {
  const project = await loadFromEnvironment();
  await runFindCustomerWorker(new FindCustomerFlow(newProjectConnection(project, ConnectionName)), stop.signal);
} catch {
  console.error('Jira Service Management Worker could not start.');
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
