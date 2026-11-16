// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newConsoleTriggerLogger, type TriggerLogLevel } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { runInvoiceChanged } from './app.js';

/** Points the example at a local Xero-compatible fake for verification only. */
const localProviderURLEnvironmentVariable = 'XERO_LOCAL_PROVIDER_URL';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);
const level = (['debug', 'info', 'warn', 'error'] as const).find(name => name === process.env.LOG_LEVEL) ?? 'info' satisfies TriggerLogLevel;
const logger = newConsoleTriggerLogger(level);
const localProviderURL = process.env[localProviderURLEnvironmentVariable];
if (localProviderURL) logger.log('warn', 'Xero API and token requests go to a local URL', { variable: localProviderURLEnvironmentVariable });
try {
  const project = await loadFromEnvironment();
  await runInvoiceChanged(project, {
    serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', logger,
    ...(localProviderURL ? { xeroOptions: { localProviderURL } } : {}),
  }, stop.signal);
} catch (error) {
  console.error(`Xero invoice-changed stopped: ${error instanceof Error ? error.message : 'unknown error'}`);
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
