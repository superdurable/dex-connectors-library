// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { defaultTriggerLogger } from '@superdurable/connectors-sdk';
import { runWebhookRequestIntake } from './app.js';

const shutdown = new AbortController();
process.once('SIGINT', () => shutdown.abort());
process.once('SIGTERM', () => shutdown.abort());
try {
  const project = await loadFromEnvironment();
  await runWebhookRequestIntake(project, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801',
    webhookHost: process.env.WEBHOOK_RECEIVER_HOST || '127.0.0.1', webhookPort: Number(process.env.WEBHOOK_RECEIVER_PORT || '8838'), logger: defaultTriggerLogger }, shutdown.signal);
} catch (error) {
  console.error('Webhook request intake stopped', error instanceof Error ? error.name : 'unknown error');
  process.exitCode = 1;
}
