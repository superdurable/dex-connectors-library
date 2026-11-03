// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it, vi } from 'vitest';
import { once } from 'node:events';
import { createServer } from 'node:http';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { OpenAIClient, pathEscape } from '../src/client.js';
import type { CreateRequest } from '../src/generated/models.js';
import { Mock, validateCreateResponseAnswer, validateCreateResponseRequest } from '../src/mock.js';

const input: CreateRequest = { Model: '', Input: 'Summarize this.', Instructions: '', StructuredOutput: null };
const wire = { id: 'resp_123', model: 'gpt-test', status: 'completed', output: [{ content: [{ type: 'output_text', text: 'Done.' }] }], usage: { input_tokens: 4, output_tokens: 2, total_tokens: 6 } };

function call(attempt = 1, previous?: unknown, streaming = false) {
  let marker = previous;
  const heartbeats: unknown[] = [];
  const texts: string[] = [];
  const progresses: unknown[] = [];
  const context = {
    attempt, deadline: new Date(Date.now() + 20_000), cancellationSignal: new AbortController().signal,
    hasLastHeartbeatValue: () => marker !== undefined, getLastHeartbeatValue: () => marker,
    async recordHeartbeat(value: unknown) { marker = value; heartbeats.push(value); },
  };
  const invocation = { id: '8a771c3d-aac1-5b5a-97fd-182c7091fa9e', idempotencyKey: '8a771c3d-aac1-5b5a-97fd-182c7091fa9e',
    operation: { connectorId: 'openai', operationId: 'createResponse' }, connection: { provider: 'openai', name: 'openai' }, context,
    reportProgress(value: unknown) { progresses.push(value); }, writeText(value: string) { texts.push(value); }, hasProgressStream: () => false, hasTextStream: () => streaming,
  } as unknown as Call;
  return { invocation, heartbeats, texts, progresses };
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

  it('keeps heartbeating during read-back when the first record fails', async () => {
    const { invocation } = call(2, { callId: '8a771c3d-aac1-5b5a-97fd-182c7091fa9e', attempt: 1, responseId: 'resp_123' });
    let heartbeatCount = 0;
    const context = invocation.context as { recordHeartbeat: (value: unknown) => Promise<void> };
    context.recordHeartbeat = async () => { if (++heartbeatCount === 1) throw new Error('transient heartbeat failure'); };
    const provider = client(async () => {
      await new Promise(resolve => setTimeout(resolve, 5500));
      return { status: 200, headers: {}, body: Buffer.from(JSON.stringify(wire)) };
    });
    vi.useFakeTimers();
    try {
      const resultPromise = provider.createResponse().invoke(invocation, input);
      await vi.advanceTimersByTimeAsync(5500);
      const result = await resultPromise;
      expect(result.kind).toBe('branch');
      expect(heartbeatCount).toBeGreaterThanOrEqual(2);
    } finally { vi.useRealTimers(); }
  });

  it('fails closed without a checkpoint after the first attempt', async () => {
    const provider = client(async () => { throw new Error('unexpected send'); });
    const result = await provider.createResponse().invoke(call(2).invocation, input);
    expect(result.kind).toBe('uncertain');
  });

  it('treats a cleared checkpoint as absent and rejects scalar checkpoints', async () => {
    const methods: string[] = [];
    const provider = client(async request => { methods.push(request.method); return { status: 200, headers: {}, body: Buffer.from(JSON.stringify(wire)) }; });
    const cleared = await provider.createResponse().invoke(call(1, null).invocation, input);
    expect(cleared.kind).toBe('branch');
    expect(methods).toEqual(['POST']);
    for (const marker of [0, '']) {
      const invalid = await provider.createResponse().invoke(call(1, marker).invocation, input);
      expect(invalid.kind).toBe('branch');
      if (invalid.kind === 'branch') expect(invalid.branch).toBe('defect');
    }
    expect(methods).toEqual(['POST']);
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
        { type: 'response.created', sequence_number: 12, response: { id: 'resp_123', status: 'in_progress' } },
        { type: 'response.output_text.delta', delta: 'Done.' },
        { type: 'response.completed', response: wire },
      ].map(event => `data: ${JSON.stringify(event)}\n\n`).join('');
      request.onResponseChunk?.(Buffer.from(events), 200, {});
      return { status: 200, headers: {}, body: Buffer.from(events) };
    });
    const { invocation, heartbeats, texts, progresses } = call(1, undefined, true);
    const result = await provider.createResponse().invoke(invocation, input);
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') expect(result.branch).toBe('completed');
    expect(texts).toEqual(['Done.']);
    expect(progresses).toContainEqual({ phase: 'response.created', message: 'in_progress', providerSequence: 12 });
    expect(heartbeats).toContainEqual({ callId: invocation.id, attempt: 1, responseId: 'resp_123' });
  });

  it('keeps create headers when a known stream is read back after transport failure', async () => {
    const methods: string[] = [];
    const provider = client(async request => {
      methods.push(request.method);
      if (request.method === 'POST') {
        request.onResponseChunk?.(Buffer.from('data: {"type":"response.created","response":{"id":"resp_123","status":"in_progress"}}\n\n'), 200, { 'x-request-id': 'req_create', 'x-ratelimit-remaining-tokens': '90' });
        throw new ProviderTransportError(false);
      }
      return { status: 200, headers: {}, body: Buffer.from(JSON.stringify(wire)) };
    });
    const result = await provider.createResponse().invoke(call(1, undefined, true).invocation, input);
    expect(methods).toEqual(['POST', 'GET']);
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') {
      expect(result.receipt.providerRequestId).toBe('req_create');
      expect(result.receipt.metadata?.['x-ratelimit-remaining-tokens']).toBe('90');
    }
  });

  it('drains an oversized known response stream before reading it back', async () => {
    const methods: string[] = [];
    let hasFinishedStream = false;
    let wasStreamClosedEarly = false;
    let didReadBackBeforeStreamFinished = false;
    const server = createServer((request, response) => {
      methods.push(request.method ?? '');
      if (request.method === 'GET') {
        didReadBackBeforeStreamFinished = !hasFinishedStream;
        response.setHeader('Content-Type', 'application/json');
        response.end(JSON.stringify(wire));
        return;
      }
      response.on('finish', () => { hasFinishedStream = true; });
      response.on('close', () => { if (!response.writableFinished) wasStreamClosedEarly = true; });
      response.setHeader('Content-Type', 'text/event-stream');
      response.write(`data: ${JSON.stringify({ type: 'response.created', response: { id: 'resp_123', status: 'in_progress' } })}\n\n`);
      setTimeout(() => response.write(`data: ${JSON.stringify({ type: 'response.output_text.delta', delta: 'x'.repeat(256) })}\n\n`), 10);
      setTimeout(() => response.end('data: [DONE]\n\n'), 20);
    });
    server.listen(0, '127.0.0.1');
    await once(server, 'listening');
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('test server address is unavailable');
    try {
      const provider = new OpenAIClient({ endpoint: `http://127.0.0.1:${address.port}`, model: 'gpt-test', maxResponseBytes: 300 }, { resolve: async () => ({ apiKey: new SecretString('test-key') }) });
      const result = await provider.createResponse().invoke(call(1, undefined, true).invocation, input);
      expect(result.kind).toBe('branch');
      if (result.kind === 'branch') expect(result.value.id).toBe('resp_123');
      expect(methods).toEqual(['POST', 'GET']);
      expect(hasFinishedStream).toBe(true);
      expect(wasStreamClosedEarly).toBe(false);
      expect(didReadBackBeforeStreamFinished).toBe(false);
    } finally { await new Promise<void>(resolve => server.close(() => resolve())); }
  });

  it('keeps create headers on an oversized body with no response ID', async () => {
    const provider = client(async () => { throw new BodyTooLargeError(200, { 'x-request-id': 'req_large', 'x-ratelimit-remaining-tokens': '80' }); });
    const result = await provider.createResponse().invoke(call().invocation, input);
    expect(result.kind).toBe('uncertain');
    if (result.kind === 'uncertain') {
      expect(result.receipt.providerRequestId).toBe('req_large');
      expect(result.receipt.metadata?.['x-ratelimit-remaining-tokens']).toBe('80');
    }
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
