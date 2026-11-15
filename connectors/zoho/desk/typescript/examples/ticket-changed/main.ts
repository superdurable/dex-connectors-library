// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newConsoleTriggerLogger, type TriggerLogLevel } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { runTicketChangeWatcher } from './app.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
// LOG_LEVEL=debug shows every poll and delivery record.
const level = (['debug', 'info', 'warn', 'error'] as const).find(name => name === process.env.LOG_LEVEL) ?? 'info' satisfies TriggerLogLevel;
// Points the Worker at a local Zoho Desk-compatible fake for verification only.
const localAPIBaseURL = process.env.ZOHO_DESK_LOCAL_API_BASE_URL;
try {
  const project = await loadFromEnvironment();
  if (localAPIBaseURL) console.error('Zoho Desk API requests go to a local base URL (ZOHO_DESK_LOCAL_API_BASE_URL)');
  await runTicketChangeWatcher(project, {
    serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', logger: newConsoleTriggerLogger(level),
    ...(localAPIBaseURL ? { zohoDeskOptions: { apiBaseURL: localAPIBaseURL } } : {}),
    onReady: () => console.error('Zoho Desk ticket change worker started'),
  }, stop.signal);
} catch (error) {
  console.error(`Zoho Desk ticket change worker stopped: ${error instanceof Error ? error.message : 'unknown error'}`);
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
