// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newConsoleTriggerLogger, type TriggerLogLevel } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { runTagNewConversationsApplication } from './app.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
// LOG_LEVEL=debug shows every delivery and every event the binding or the Flow filter skips.
const level = (['debug', 'info', 'warn', 'error'] as const).find(name => name === process.env.LOG_LEVEL) ?? 'info' satisfies TriggerLogLevel;
const [host, port] = (process.env.WEBHOOK_BIND_ADDRESS || '127.0.0.1:8872').split(':');
try {
  const project = await loadFromEnvironment();
  await runTagNewConversationsApplication(project, {
    serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', webhookHost: host || '127.0.0.1', webhookPort: Number(port || 8872),
    logger: newConsoleTriggerLogger(level),
    onReady: ready => { console.error(`Help Scout webhook listening at ${ready.webhookURL}`); },
  }, stop.signal);
} catch (error) {
  console.error(`Help Scout new-conversation tagging stopped: ${error instanceof Error ? error.message : 'unknown error'}`);
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
