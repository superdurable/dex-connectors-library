// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newConsoleTriggerLogger, type TriggerLogLevel } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { runThreadApprovalApplication } from './app.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
// LOG_LEVEL=debug shows every Slack message the Triggers ignore and every delivery.
const level = (['debug', 'info', 'warn', 'error'] as const).find(name => name === process.env.LOG_LEVEL) ?? 'info' satisfies TriggerLogLevel;
const [host, port] = (process.env.TRIGGER_BIND_ADDRESS || '127.0.0.1:8839').split(':');
try {
  const project = await loadFromEnvironment();
  await runThreadApprovalApplication(project, {
    serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', triggerHost: host || '127.0.0.1', triggerPort: Number(port || 8839),
    logger: newConsoleTriggerLogger(level),
  }, stop.signal);
} catch (error) {
  console.error(`Slack thread approval stopped: ${error instanceof Error ? error.message : 'unknown error'}`);
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
