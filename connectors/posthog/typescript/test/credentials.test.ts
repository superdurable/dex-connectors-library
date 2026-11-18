// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { PostHogClient } from '../src/client.js';

const call = {
  id: '2c9e4b1a-6d3f-5e8a-9b0c-7d1e2f3a4b5c', idempotencyKey: '2c9e4b1a-6d3f-5e8a-9b0c-7d1e2f3a4b5c',
  connection: { provider: 'posthog', name: 'posthog' },
  operation: { connectorId: 'posthog', operationId: 'captureEvent' },
  context: { cancellationSignal: new AbortController().signal },
} as Call;

it.each([
  ['broker unavailable', new CredentialBrokerUnavailableError(), 'retry'],
  ['static not configured', new Error('connection is not configured'), 'retry'],
  ['unlisted error', new RangeError('credential store hiccup'), 'retry'],
  ['reauthorization', new ReauthorizationRequiredError(), 'branch'],
  ['object not found', new ObjectNotFoundError(), 'branch'],
  ['connection not selected', new ConnectionNotSelectedError(), 'branch'],
  ['broker rejected', new CredentialBrokerRejectedError(), 'branch'],
])('classifies a %s credential failure before any capture request', async (_name, error, kind) => {
  let requests = 0;
  const client = new PostHogClient({}, { resolve: async () => { throw error; } }, {
    send: async () => { requests++; throw new Error('no request may be sent'); },
  });
  const result = await client.captureEvent().invoke(call, { event: 'Signed Up', distinctId: 'member-1', timestamp: '2026-10-29T00:00:00Z' });
  expect(result.kind).toBe(kind);
  if (result.kind === 'branch') expect(result.branch).toBe('defect');
  expect(requests).toBe(0);
});
