// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { MailgunClient } from '../src/client.js';

const call = {
  id: '614a05e0-1776-5bad-a744-85a79d25c628',
  connection: { provider: 'mailgun', name: 'mailgun' },
  operation: { connectorId: 'mailgun', operationId: 'getEvents' },
  context: { cancellationSignal: new AbortController().signal },
} as Call;

it('returns terminal authentication defect before sending for invalid credentials', async () => {
  let requests = 0;
  const client = new MailgunClient({}, { resolve: async () => ({ apiKey: new SecretString('') }) }, {
    send: async () => { requests++; throw new Error('unexpected request'); },
  });
  const attempt = await client.getEvents().invoke(call, { domain: 'example.test' });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') {
    expect(attempt.branch).toBe('defect');
    expect(attempt.failure?.kind).toBe('AUTHENTICATION');
  }
  expect(requests).toBe(0);
});

it('treats a refused local provider connection as retry', async () => {
  const client = new MailgunClient({}, { resolve: async () => ({ apiKey: new SecretString('test-key-123') }) }, {
    send: async () => { throw new Error('ECONNREFUSED'); },
  });
  const attempt = await client.getEvents().invoke(call, { domain: 'example.test' });
  expect(attempt.kind).toBe('retry');
  if (attempt.kind === 'retry') expect(attempt.failure.kind).toBe('AVAILABILITY');
});

it('rejects a trailing newline in a domain before contacting the provider', async () => {
  let requests = 0;
  const client = new MailgunClient({}, { resolve: async () => ({ apiKey: new SecretString('test-key-123') }) }, {
    send: async () => { requests++; throw new Error('unexpected request'); },
  });
  const attempt = await client.getEvents().invoke(call, { domain: 'example.test\n' });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') expect(attempt.branch).toBe('defect');
  expect(requests).toBe(0);
});

it('classifies an oversized successful response as invalidResponse', async () => {
  const client = new MailgunClient({}, { resolve: async () => ({ apiKey: new SecretString('test-key-123') }) }, {
    send: async () => { throw new BodyTooLargeError(200); },
  });
  const attempt = await client.getEvents().invoke(call, { domain: 'example.test' });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') {
    expect(attempt.branch).toBe('invalidResponse');
    expect(attempt.failure?.kind).toBe('RESPONSE_TOO_LARGE');
  }
});
