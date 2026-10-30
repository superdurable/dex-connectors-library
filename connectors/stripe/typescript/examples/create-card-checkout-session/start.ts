// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type BlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, CreateCheckoutSessionInputSchema, type CheckoutSession } from '@superdurable/connector-stripe';
import { CardCheckoutFlow, ConnectionName, type CardCheckoutInput } from './flow/workflow.js';

async function startCardCheckout(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('card Checkout input JSON file is required');
  const parsed = CreateCheckoutSessionInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')));
  const input: CardCheckoutInput = {
    clientReferenceId: parsed.clientReferenceId, customerEmail: parsed.customerEmail,
    successUrl: parsed.successUrl, cancelUrl: parsed.cancelUrl, currency: parsed.currency,
    unitAmount: parsed.unitAmount, productName: parsed.productName,
    ...(parsed.metadata ? { metadata: parsed.metadata } : {}),
    ...(parsed.expiresAt ? { expiresAt: parsed.expiresAt } : {}),
  };
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  const flow = new CardCheckoutFlow(connection);
  const directory = await mkdtemp(join(tmpdir(), 'dex-stripe-card-client-'));
  let cache: BlobCache | undefined;
  let client: Client | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    client = new Client(new Registry([flow]), cache, {
      serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801',
    });
    const flowId = `stripe-card-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('card Checkout Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<CheckoutSession>())}\n`);
  } finally {
    try { await client?.close(); } finally {
      cache?.close();
      await rm(directory, { recursive: true, force: true });
    }
  }
}

try { await startCardCheckout(); } catch {
  console.error('Stripe card Checkout Flow could not start or complete.');
  process.exitCode = 1;
}
