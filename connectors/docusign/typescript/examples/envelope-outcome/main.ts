// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newConsoleTriggerLogger, type TriggerLogLevel } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { runEnvelopeOutcomeApplication } from './app.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
// LOG_LEVEL=debug shows every delivery and every event the binding filters.
const level = (['debug', 'info', 'warn', 'error'] as const).find(name => name === process.env.LOG_LEVEL) ?? 'info' satisfies TriggerLogLevel;
// host:port as Go's net.Listen takes it, including a bracketed IPv6 host such as [::1]:8862.
const bindAddress = process.env.CONNECT_BIND_ADDRESS || '127.0.0.1:8862';
const host = bindAddress.slice(0, Math.max(bindAddress.lastIndexOf(':'), 0)).replace(/^\[(.*)\]$/, '$1');
const port = bindAddress.slice(bindAddress.lastIndexOf(':') + 1);
try {
  const project = await loadFromEnvironment();
  await runEnvelopeOutcomeApplication(project, {
    serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', connectHost: host || '127.0.0.1', connectPort: Number(port || 8862),
    logger: newConsoleTriggerLogger(level),
  }, stop.signal);
} catch (error) {
  console.error(`DocuSign envelope outcome stopped: ${error instanceof Error ? error.message : 'unknown error'}`);
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
