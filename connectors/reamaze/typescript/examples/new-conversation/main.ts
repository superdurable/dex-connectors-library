// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newConsoleTriggerLogger, type TriggerLogLevel } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { runNewConversationWorker } from './app.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
// LOG_LEVEL=debug also shows every delivery.
const level = (['debug', 'info', 'warn', 'error'] as const).find(name => name === process.env.LOG_LEVEL) ?? 'info' satisfies TriggerLogLevel;
// REAMAZE_LOCAL_API_BASE_URL points the connection at a local Re:amaze stand-in for verification only.
const localAPIBaseURL = process.env.REAMAZE_LOCAL_API_BASE_URL;
try {
  const project = await loadFromEnvironment();
  await runNewConversationWorker(project, {
    serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', logger: newConsoleTriggerLogger(level),
    ...(localAPIBaseURL ? { reamazeOptions: { apiBaseURL: localAPIBaseURL } } : {}),
  }, stop.signal);
} catch (error) {
  console.error(`Re:amaze new conversation worker stopped: ${error instanceof Error ? error.message : 'unknown error'}`);
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
