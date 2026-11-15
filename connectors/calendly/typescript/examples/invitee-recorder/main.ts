// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newConsoleTriggerLogger, type TriggerLogLevel } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { runInviteeRecorder } from './app.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
// LOG_LEVEL=debug shows every delivery and every booking the admission rule filters.
const level = (['debug', 'info', 'warn', 'error'] as const).find(name => name === process.env.LOG_LEVEL) ?? 'info' satisfies TriggerLogLevel;
const [host, port] = (process.env.WEBHOOK_BIND_ADDRESS || '127.0.0.1:8852').split(':');
try {
  const project = await loadFromEnvironment();
  await runInviteeRecorder(project, {
    serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', webhookHost: host || '127.0.0.1', webhookPort: Number(port || 8852),
    logger: newConsoleTriggerLogger(level),
    onListening: url => console.error(`invitee-recorder webhook listening at ${url}`),
  }, stop.signal);
} catch (error) {
  console.error(`Calendly invitee recorder stopped: ${error instanceof Error ? error.message : 'unknown error'}`);
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
