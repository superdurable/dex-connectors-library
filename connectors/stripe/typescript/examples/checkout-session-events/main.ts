// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newConsoleTriggerLogger, type TriggerLogLevel } from '@superdurable/connectors-sdk';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { runCheckoutSessionApp } from './app.js';

const stop = new AbortController();
const onSignal = (): void => stop.abort();
process.once('SIGINT', onSignal);
process.once('SIGTERM', onSignal);
const level = (['debug', 'info', 'warn', 'error'] as const).find(name => name === process.env.LOG_LEVEL) ?? 'info' satisfies TriggerLogLevel;
const [host, port] = (process.env.STRIPE_WEBHOOK_BIND_ADDRESS || '127.0.0.1:8854').split(':');
try {
  const project = await loadFromEnvironment();
  await runCheckoutSessionApp(project, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', webhookHost: host || '127.0.0.1', webhookPort: Number(port || 8854), logger: newConsoleTriggerLogger(level), onListening: url => console.error(`Stripe webhook listening at ${url}`) }, stop.signal);
} catch (error) {
  console.error(`Stripe Checkout Session receiver stopped: ${error instanceof Error ? error.message : 'unknown error'}`);
  process.exitCode = 1;
} finally {
  process.off('SIGINT', onSignal);
  process.off('SIGTERM', onSignal);
}
