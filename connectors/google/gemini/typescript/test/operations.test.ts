// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { GeminiClient, type GeminiRequestSender } from '../src/client.js';
import type { GenerateContentRequest } from '../src/generated/models.js';

const call = { id: 'gemini-test', connection: { provider: 'google', name: 'gemini-default' },
  operation: { connectorId: 'gemini', operationId: 'generateContent' },
  context: { flowId: 'gemini-test-flow', cancellationSignal: new AbortController().signal } as Call['context'],
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
} as Call;
const input: GenerateContentRequest = { contents: [{ role: 'user', parts: [{ text: 'Summarize this' }] }] };

it('sends the API key only in the header and maps non-thought text', async () => {
  let observedURL = '';
  let observedKey = '';
  const send: GeminiRequestSender = async request => {
    observedURL = request.url;
    observedKey = request.headers?.['x-goog-api-key'] ?? '';
    return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ candidates: [{ finishReason: 'STOP', content: { parts: [{ text: 'hidden', thought: true }, { text: 'Done' }] } }], responseId: 'resp-test' })) };
  };
  const client = new GeminiClient({}, { resolve: async () => ({ apiKey: new SecretString('test-key') }) }, { send });
  const attempt = await client.generateContent().invoke(call, input);
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') { expect(attempt.branch).toBe('generated'); expect(attempt.value.text).toBe('Done'); }
  expect(observedURL).toContain('/models/gemini-3.5-flash-lite:generateContent');
  expect(observedURL).not.toContain('test-key');
  expect(observedKey).toBe('test-key');
});

it('selects defect for invalid input before resolving credentials', async () => {
  let resolutionCount = 0;
  const client = new GeminiClient({}, { resolve: async () => { resolutionCount++; return { apiKey: new SecretString('test-key') }; } });
  const attempt = await client.generateContent().invoke(call, { contents: [{ role: 'user', parts: [{ text: '' }] }] });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') { expect(attempt.branch).toBe('defect'); expect(attempt.failure?.kind).toBe('VALIDATION'); }
  expect(resolutionCount).toBe(0);
});

it('classifies credential failure as terminal and provider unavailability as Retry', async () => {
  const unavailable = new GeminiClient({}, { resolve: async () => { throw new Error('secret unavailable'); } });
  const credentialAttempt = await unavailable.generateContent().invoke(call, input);
  expect(credentialAttempt.kind).toBe('branch');
  if (credentialAttempt.kind === 'branch') { expect(credentialAttempt.branch).toBe('defect'); expect(credentialAttempt.failure?.kind).toBe('AUTHENTICATION'); }
  const retrying = new GeminiClient({}, { resolve: async () => ({ apiKey: new SecretString('test-key') }) }, {
    send: async () => ({ status: 503, headers: { 'retry-after': '2' }, body: Buffer.from('{"error":{"status":"UNAVAILABLE"}}') }),
  });
  const providerAttempt = await retrying.generateContent().invoke(call, input);
  expect(providerAttempt.kind).toBe('retry');
  if (providerAttempt.kind === 'retry') { expect(providerAttempt.failure.kind).toBe('AVAILABILITY'); expect(providerAttempt.retryAfterMs).toBe(2_000); }
});

it('retries an error body larger than the configured successful response limit', async () => {
  const client = new GeminiClient({ maxResponseBytes: 512 }, { resolve: async () => ({ apiKey: new SecretString('test-key') }) }, {
    send: async request => {
      expect(request.maxResponseBytes).toBe(65_536);
      return { status: 429, headers: { 'retry-after': '2' }, body: Buffer.from(JSON.stringify({ error: { status: 'RESOURCE_EXHAUSTED', padding: 'x'.repeat(1_024) } })) };
    },
  });
  const attempt = await client.generateContent().invoke(call, input);
  expect(attempt).toMatchObject({ kind: 'retry', failure: { kind: 'RATE_LIMIT', message: 'provider returned HTTP 429 RESOURCE_EXHAUSTED' }, retryAfterMs: 2_000 });
});

it('classifies responses beyond the sender limit by HTTP status', async () => {
  const credentials = { resolve: async () => ({ apiKey: new SecretString('test-key') }) };
  const retrying = new GeminiClient({ maxResponseBytes: 512 }, credentials, {
    send: async () => { throw new BodyTooLargeError(503, { 'retry-after': '3' }); },
  });
  expect(await retrying.generateContent().invoke(call, input)).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' }, retryAfterMs: 3_000 });
  const rejected = new GeminiClient({ maxResponseBytes: 512 }, credentials, {
    send: async () => { throw new BodyTooLargeError(403, {}); },
  });
  expect(await rejected.generateContent().invoke(call, input)).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHORIZATION' } });
  const oversizedSuccess = new GeminiClient({ maxResponseBytes: 512 }, credentials, {
    send: async () => ({ status: 200, headers: {}, body: Buffer.alloc(513) }),
  });
  expect(await oversizedSuccess.generateContent().invoke(call, input)).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'RESPONSE_TOO_LARGE' } });
});
