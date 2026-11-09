// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { PhoneNumbersFlow } from '../../examples/provider-operations/phone-numbers-flow.js';
import { TemplateMessageFlow } from '../../examples/provider-operations/template-message-flow.js';
import { ConnectionName, TextMessageFlow } from '../../examples/provider-operations/text-message-flow.js';
import { WhatsAppBusinessClient } from '../../src/client.js';
import { newConnection, type PhoneNumbersPage, type SentMessage } from '../../src/index.js';

it('runs all WhatsApp operations through real Dex retry and uncertainty boundaries', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let queryRequests = 0;
  let textRequests = 0;
  let templateRequests = 0;
  const server = createServer(async (request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-system-user-token') { response.writeHead(400).end(); return; }
    if (request.method === 'GET') {
      if (!request.url?.startsWith('/v25.0/123456789/phone_numbers?')) { response.writeHead(400).end(); return; }
      queryRequests++;
      if (queryRequests === 1) { response.writeHead(503).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ data: [{ id: '1906385232743451', verified_name: 'Example Store', display_phone_number: '+1 631-555-5555' }] }));
      return;
    }
    if (request.method !== 'POST' || request.url !== '/v25.0/1906385232743451/messages') { response.writeHead(400).end(); return; }
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const payload = JSON.parse(Buffer.concat(chunks).toString()) as { type?: string };
    if (payload.type === 'text') {
      textRequests++;
      if (textRequests === 1) { response.writeHead(503).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ messaging_product: 'whatsapp', messages: [{ id: 'wamid.example-text' }] }));
      return;
    }
    if (payload.type === 'template') { templateRequests++; response.writeHead(500).end(); return; }
    response.writeHead(400).end();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('WhatsApp fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-whatsapp-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new WhatsAppBusinessClient({ baseUrl: `http://127.0.0.1:${address.port}/v25.0` }, {
    resolve: async () => ({ accessToken: new SecretString('synthetic-system-user-token') }),
  });
  const connection = newConnection(provider, { provider: 'whatsapp-business', name: ConnectionName });
  const phoneNumbersFlow = new PhoneNumbersFlow(connection);
  const textMessageFlow = new TextMessageFlow(connection);
  const templateMessageFlow = new TemplateMessageFlow(connection);
  const registry = new Registry([phoneNumbersFlow, textMessageFlow, templateMessageFlow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const queryID = `whatsapp-ts-query-${randomUUID()}`;
    await client.startFlow(phoneNumbersFlow, queryID, { businessAccountId: '123456789', after: 'cursor' });
    const query = await client.waitForFlow(queryID, 60_000);
    expect(query.status, query.errorMessage).toBe('completed');
    expect(query.singleOutput<PhoneNumbersPage>().numbers?.[0]?.id).toBe('1906385232743451');
    expect(queryRequests).toBe(2);

    const textID = `whatsapp-ts-text-${randomUUID()}`;
    await client.startFlow(textMessageFlow, textID, { phoneNumberId: '1906385232743451', recipient: '16315551234', text: 'Hello' });
    const text = await client.waitForFlow(textID, 60_000);
    expect(text.status, text.errorMessage).toBe('completed');
    expect(text.singleOutput<SentMessage>().messageId).toBe('wamid.example-text');
    expect(textRequests).toBe(2);

    const templateID = `whatsapp-ts-template-${randomUUID()}`;
    await client.startFlow(templateMessageFlow, templateID, { phoneNumberId: '1906385232743451', recipient: '16315551234', templateName: 'order_update', languageCode: 'en_US', bodyParameters: ['A123'] });
    const template = await client.waitForFlow(templateID, 60_000);
    expect(template.status).toBe('failed');
    expect(template.errorMessage).toContain('uncertain');
    expect(templateRequests).toBe(1);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
