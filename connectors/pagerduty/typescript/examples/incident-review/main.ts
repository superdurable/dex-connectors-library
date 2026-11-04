// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-pagerduty';
import { ConnectionName, IncidentLookupFlow, IncidentReviewFlow } from './flow/workflow.js';
import { runIncidentReviewWorker } from './worker-runtime.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
try {
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  await runIncidentReviewWorker(new IncidentReviewFlow(connection), new IncidentLookupFlow(connection), stop.signal);
} catch { console.error('PagerDuty Worker could not start or stopped with an error.'); process.exitCode = 1; }
finally { process.off('SIGINT', handleSignal); process.off('SIGTERM', handleSignal); }
