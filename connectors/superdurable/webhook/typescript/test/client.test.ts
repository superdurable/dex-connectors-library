// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { CredentialBrokerUnavailableError } from '@superdurable/connectors-sdk/projectconfig';
import { AddressRefusal, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WebhookClient } from '../src/client.js';

const call = {
  id: '1df9f2d3-800f-54dc-898c-77640b04c7c3',
  idempotencyKey: 'msg_1df9f2d3800f54dc898c77640b04c7c3',
  connection: { provider: 'webhook', name: 'forms' }, operation: { connectorId: 'webhook', operationId: 'sendEvent' },
  context: { cancellationSignal: new AbortController().signal },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
} as unknown as Call;
const input = { payload: { type: 'form.submitted' } };

it('retries broker outages and fails missing credentials', async () => {
  const unavailable = new WebhookClient({ deliveryUrl: 'https://receiver.example/events' }, { resolve: async () => { throw new CredentialBrokerUnavailableError(); } });
  const retry = await unavailable.sendEvent().invoke(call, input);
  expect(retry.kind).toBe('retry');
  expect(retry.failure?.kind).toBe('AVAILABILITY');
  const missing = new WebhookClient({ deliveryUrl: 'https://receiver.example/events' }, { resolve: async () => { throw new Error('secret'); } });
  const defect = await missing.sendEvent().invoke(call, input);
  expect(defect.kind).toBe('branch');
  if (defect.kind === 'branch') expect(defect.branch).toBe('defect');
  expect(defect.failure?.kind).toBe('AUTHENTICATION');
});

it('classifies unsent requests and lost responses separately', async () => {
  for (const [error, kind] of [[new AddressRefusal('test'), 'AVAILABILITY'], [new ProviderTransportError(true), 'AVAILABILITY'], [new ProviderTransportError(false), 'TRANSPORT']] as const) {
    const client = new WebhookClient({ deliveryUrl: 'https://receiver.example/events' }, {
      resolve: async () => ({ signingSecret: new SecretString('test-secret') }),
    }, { send: async () => { throw error; } });
    const attempt = await client.sendEvent().invoke(call, input);
    expect(attempt.kind).toBe('retry');
    expect(attempt.failure?.kind).toBe(kind);
  }
});

it('rejects an invalid signing secret before dispatch', async () => {
  let isSent = false;
  const client = new WebhookClient({ deliveryUrl: 'https://receiver.example/events' }, {
    resolve: async () => ({ signingSecret: new SecretString('whsec_?') }),
  }, { send: async () => { isSent = true; throw new Error('unexpected send'); } });
  const attempt = await client.sendEvent().invoke(call, input);
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') expect(attempt.branch).toBe('defect');
  expect(attempt.failure?.kind).toBe('VALIDATION');
  expect(isSent).toBe(false);
});
