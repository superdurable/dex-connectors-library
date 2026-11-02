// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
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
