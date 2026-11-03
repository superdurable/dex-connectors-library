// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { once } from 'node:events';
import { createServer } from 'node:http';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { OpenAIClient, pathEscape } from '../src/client.js';
import type { CreateRequest } from '../src/generated/models.js';
import { Mock, validateCreateResponseAnswer, validateCreateResponseRequest } from '../src/mock.js';

const input: CreateRequest = { Model: '', Input: 'Summarize this.', Instructions: '', StructuredOutput: null };
const wire = { id: 'resp_123', model: 'gpt-test', status: 'completed', output: [{ content: [{ type: 'output_text', text: 'Done.' }] }], usage: { input_tokens: 4, output_tokens: 2, total_tokens: 6 } };

function call(attempt = 1, previous?: unknown, streaming = false) {
  let marker = previous;
  const heartbeats: unknown[] = [];
  const texts: string[] = [];
  const context = {
    attempt, deadline: new Date(Date.now() + 20_000), cancellationSignal: new AbortController().signal,
    hasLastHeartbeatValue: () => marker !== undefined, getLastHeartbeatValue: () => marker,
    async recordHeartbeat(value: unknown) { marker = value; heartbeats.push(value); },
  };
  const invocation = { id: '8a771c3d-aac1-5b5a-97fd-182c7091fa9e', idempotencyKey: '8a771c3d-aac1-5b5a-97fd-182c7091fa9e',
    operation: { connectorId: 'openai', operationId: 'createResponse' }, connection: { provider: 'openai', name: 'openai' }, context,
    reportProgress() {}, writeText(value: string) { texts.push(value); }, hasProgressStream: () => false, hasTextStream: () => streaming,
  } as unknown as Call;
  return { invocation, heartbeats, texts };
}

function client(send: (request: ProviderRequest) => Promise<ProviderResponse>): OpenAIClient {
  return new OpenAIClient({ endpoint: 'http://localhost:34080/v1', model: 'gpt-test' }, { resolve: async () => ({ apiKey: new SecretString('test-key') }) }, { send });
}

describe('createResponse checkpoint recovery', () => {
  it('reads back a checkpointed response without another POST', async () => {
    const methods: string[] = [];
    const provider = client(async request => { methods.push(request.method); return { status: 200, headers: {}, body: Buffer.from(JSON.stringify(wire)) }; });
    const { invocation, heartbeats } = call(2, { callId: '8a771c3d-aac1-5b5a-97fd-182c7091fa9e', attempt: 1, responseId: 'resp_123' });
    const result = await provider.createResponse().invoke(invocation, input);
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.value.id).toBe('resp_123');
    expect(methods).toEqual(['GET']);
    expect(heartbeats).toContainEqual({ callId: invocation.id, attempt: 2, responseId: 'resp_123' });
  });

  it('fails closed without a checkpoint after the first attempt', async () => {
    const provider = client(async () => { throw new Error('unexpected send'); });
    const result = await provider.createResponse().invoke(call(2).invocation, input);
    expect(result.kind).toBe('uncertain');
  });

  it('retries only a transport failure proved to be before-send', async () => {
    const provider = client(async () => { throw new ProviderTransportError(true); });
    const { invocation, heartbeats } = call();
    const result = await provider.createResponse().invoke(invocation, input);
    expect(result.kind).toBe('retry');
    expect(heartbeats.at(-1)).toEqual({ callId: invocation.id, attempt: 1, canResend: true });
  });

  it('decodes streamed text and checkpoints its response ID', async () => {
    const provider = client(async request => {
      const events = [
        { type: 'response.created', response: { id: 'resp_123', status: 'in_progress' } },
        { type: 'response.output_text.delta', delta: 'Done.' },
        { type: 'response.completed', response: wire },
      ].map(event => `data: ${JSON.stringify(event)}\n\n`).join('');
      request.onResponseChunk?.(Buffer.from(events), 200, {});
      return { status: 200, headers: {}, body: Buffer.from(events) };
    });
    const { invocation, heartbeats, texts } = call(1, undefined, true);
    const result = await provider.createResponse().invoke(invocation, input);
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('completed');
    expect(texts).toEqual(['Done.']);
    expect(heartbeats).toContainEqual({ callId: invocation.id, attempt: 1, responseId: 'resp_123' });
  });
});

it('preserves Go path escaping and rejects wrong-typed provider fields', async () => {
  expect(pathEscape("a!b/c$&+=:@,;?~")).toBe('a%21b%2Fc$&+=:@%2C%3B%3F~');
  const provider = client(async () => ({ status: 200, headers: {}, body: Buffer.from('{"id":12,"status":"completed"}') }));
  const result = await provider.retrieveResponse().invoke(call().invocation, { ResponseID: 'resp_123' });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') expect(result.branch).toBe('invalidResponse');
});

it('classifies oversized error bodies by status and oversized success bodies as invalid', async () => {
  const server = createServer((request, response) => {
    response.writeHead(request.url?.endsWith('/rate') ? 429 : 200, { 'Retry-After': '2' });
    response.end('x'.repeat(100));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('test server address is unavailable');
  try {
    const provider = new OpenAIClient({ endpoint: `http://127.0.0.1:${address.port}`, maxResponseBytes: 10 }, { resolve: async () => ({ apiKey: new SecretString('test-key') }) });
    const limited = await provider.retrieveResponse().invoke(call().invocation, { ResponseID: 'rate' });
    expect(limited.kind).toBe('retry');
    if (limited.kind === 'retry') expect(limited.failure.kind).toBe('RATE_LIMIT');
    const oversized = await provider.retrieveResponse().invoke(call().invocation, { ResponseID: 'large' });
    expect(oversized.kind).toBe('branch');
    if (oversized.kind === 'branch') { expect(oversized.branch).toBe('invalidResponse'); expect(oversized.failure?.kind).toBe('RESPONSE_TOO_LARGE'); }
  } finally { await new Promise<void>(resolve => server.close(() => resolve())); }
});

it('uses portable schema checks only in mock answer validation', () => {
  const request: CreateRequest = { ...input, StructuredOutput: { name: 'summary', schema: { type: 'object', properties: { title: { type: 'string' } }, required: ['title'], additionalProperties: false }, strict: true } };
  expect(validateCreateResponseRequest(request)).toBeUndefined();
  expect(() => validateCreateResponseAnswer(request, { id: 'resp_1', model: 'gpt-test', status: 'completed', outputText: '{"title":"ok"}', usage: { inputTokens: 0, cachedInputTokens: 0, outputTokens: 0, reasoningOutputTokens: 0, totalTokens: 0 } })).not.toThrow();
  expect(() => validateCreateResponseAnswer(request, { id: 'resp_1', model: 'gpt-test', status: 'completed', outputText: '{"other":"bad"}', usage: { inputTokens: 0, cachedInputTokens: 0, outputTokens: 0, reasoningOutputTokens: 0, totalTokens: 0 } })).toThrow();
  expect(new Mock('openai').connection().reference.name).toBe('openai');
});
