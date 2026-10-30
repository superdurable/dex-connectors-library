// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectStripeConnection } from '@superdurable/connector-stripe';
import { CardCheckoutFlow, ConnectionName } from './flow/workflow.js';
import { runCardCheckoutWorker } from './worker-runtime.js';

const stop = new AbortController();
const handleSignal = (): void => { stop.abort(); };
process.once('SIGINT', handleSignal);
process.once('SIGTERM', handleSignal);

try {
  const project = await loadFromEnvironment();
  const connection = newProjectStripeConnection(project, ConnectionName);
  await runCardCheckoutWorker(new CardCheckoutFlow(connection), stop.signal);
} catch {
  console.error('Stripe card Checkout Worker could not start or stopped with an error.');
  process.exitCode = 1;
} finally {
  process.off('SIGINT', handleSignal);
  process.off('SIGTERM', handleSignal);
}
