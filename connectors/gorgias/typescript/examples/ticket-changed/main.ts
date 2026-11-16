// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newConsoleTriggerLogger, type TriggerLogLevel } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { runTicketChanged } from './app.js';

/** Points the example at a local Gorgias-compatible fake for verification only. */
const localAPIBaseURLEnvironmentVariable = 'GORGIAS_LOCAL_API_BASE_URL';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
const level = (['debug', 'info', 'warn', 'error'] as const).find(name => name === process.env.LOG_LEVEL) ?? 'info' satisfies TriggerLogLevel;
const logger = newConsoleTriggerLogger(level);
const localAPIBaseURL = process.env[localAPIBaseURLEnvironmentVariable];
if (localAPIBaseURL) logger.log('warn', 'Gorgias API requests go to a local base URL', { variable: localAPIBaseURLEnvironmentVariable });
try {
  const project = await loadFromEnvironment();
  await runTicketChanged(project, {
    serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', logger,
    ...(localAPIBaseURL ? { gorgiasOptions: { apiBaseURL: localAPIBaseURL } } : {}),
  }, stop.signal);
} catch (error) {
  console.error(`Gorgias ticket-changed stopped: ${error instanceof Error ? error.message : 'unknown error'}`);
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
