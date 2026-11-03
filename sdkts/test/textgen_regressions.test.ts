// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { afterEach, expect, it } from 'vitest';
import { createServer, type Server, type ServerResponse } from 'node:http';
import type { AsyncContext } from '@superdurable/dex';
import { createCall } from '../src/credentials.js';
import { textGenerationBranchDefinitions, type TextGenerationRequest } from '../src/textgen/contract.js';
import { newWireFormat } from '../src/textgen/openaichat/index.js';
import { newTextGenerationQuery, type TextGenerationQueryConfig } from '../src/textgen/query.js';
import { blockedOutcome, type WireFormat } from '../src/textgen/wire_format.js';

let server: Server | undefined;
afterEach(async () => {
  if (server) await new Promise<void>(resolve => server?.close(() => resolve()));
  server = undefined;
});

const request = (): TextGenerationRequest => ({ messages: [{ role: 'user', text: 'hello' }] });
const call = (writeText?: (text: string) => void) => {
  const providerCall = createCall({ flowId: 'textgen-regression', stepExecutionId: 'generate', attempt: 1, recordHeartbeat: async () => {} } as unknown as AsyncContext,
    { provider: 'chat', name: 'regression' }, { connectorId: 'chat', operationId: 'generateText' });
  return {
    context: providerCall.context, id: providerCall.id, connection: providerCall.connection,
    operation: providerCall.operation, reportProgress: providerCall.reportProgress,
    hasProgressStream: providerCall.hasProgressStream,
    hasTextStream: () => !!writeText, writeText: writeText ?? (() => {}),
  };
};

async function startProvider(reply: (response: ServerResponse) => void | Promise<void>): Promise<string> {
  server = createServer(async (incoming, response) => {
    for await (const chunk of incoming) { void chunk; }
    await reply(response);
  });
  await new Promise<void>(resolve => server?.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('missing provider address');
  return `http://127.0.0.1:${address.port}`;
}

function query(baseURL: string, wireFormat: WireFormat = newWireFormat({ providerName: 'chat', streaming: 'always' }), change?: (config: TextGenerationQueryConfig) => void) {
  const config: TextGenerationQueryConfig = {
    definition: { operation: { connectorId: 'chat', operationId: 'generateText' }, branches: textGenerationBranchDefinitions(), stepDefaults: { executeDurability: 'sync', executeMethodTimeoutMs: 60_000 } },
    wireFormat, baseURL, connectionModel: 'chat-model', requestTimeoutMs: 10_000,
    resolveCredential: () => 'test-only', maxResponseBytes: 1 << 16, maxStreamEventBytes: 4096,
  };
  change?.(config);
  return newTextGenerationQuery(config);
}

it.each([
  { parts: undefined, providerFinishReason: 'stop' },
  { parts: [{ text: 'ok' }], servedModel: 5, providerFinishReason: 'stop' },
  { parts: [{ text: 5 }], providerFinishReason: 'stop' },
])('classifies malformed decoded responses after a complete 2xx body without retry', async decoded => {
  const baseURL = await startProvider(response => { response.end('{}'); });
  const format = newWireFormat({ providerName: 'chat' });
  format.decodeResponse = () => decoded as never;
  const outcome = await query(baseURL, format).invoke(call(), request());
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') expect(outcome.branch).toBe('invalidResponse');
});

it.each(['constructor', '__proto__'])('does not look up inherited finish mapping %s', async token => {
  const baseURL = await startProvider(response => { response.end('{}'); });
  const format = newWireFormat({ providerName: 'chat' });
  format.decodeResponse = () => ({ parts: [{ text: 'ok' }], providerFinishReason: token });
  const outcome = await query(baseURL, format).invoke(call(), request());
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') expect(outcome.branch).toBe('invalidResponse');
});

it('treats null optional request fields as absent', async () => {
  const baseURL = await startProvider(response => {
    response.setHeader('Content-Type', 'application/json');
    response.end('{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}');
  });
  const outcome = await query(baseURL, newWireFormat({ providerName: 'chat' })).invoke(call(),
    { ...request(), temperature: null, structuredOutput: null } as unknown as TextGenerationRequest);
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') expect(outcome.branch).toBe('generated');
});

it('classifies a truncated 400 body by status', async () => {
  const baseURL = await startProvider(response => {
    response.writeHead(400, { 'Content-Type': 'application/json', 'Content-Length': '100' });
    response.flushHeaders();
    response.write('{"error":');
    setImmediate(() => response.destroy());
  });
  const outcome = await query(baseURL).invoke(call(), request());
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') expect(outcome.branch).toBe('providerRejected');
});

it('retries an empty 200 event stream', async () => {
  const baseURL = await startProvider(response => { response.setHeader('Content-Type', 'text/event-stream'); response.end(); });
  const outcome = await query(baseURL).invoke(call(), request());
  expect(outcome.kind).toBe('retry');
  expect(outcome.failure?.kind).toBe('TRANSPORT');
});

it('accepts null choice index and ignores bytes after DONE', async () => {
  const baseURL = await startProvider(response => {
    response.setHeader('Content-Type', 'text/event-stream');
    response.end('data: {"choices":[{"index":null,"delta":{"content":"ok"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\ndata: garbage\n\n');
  });
  const outcome = await query(baseURL).invoke(call(), request());
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') { expect(outcome.branch).toBe('generated'); expect(outcome.value.text).toBe('ok'); }
});

it('preserves a reported stream error before a later socket failure', async () => {
  const baseURL = await startProvider(response => {
    response.setHeader('Content-Type', 'text/event-stream');
    response.write('data: {"error":{"code":"server_error"}}\n\n');
    setTimeout(() => response.destroy(), 100);
  });
  const outcome = await query(baseURL).invoke(call(), request());
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') expect(outcome.branch).toBe('invalidResponse');
});

it('stops reading after a failed text Stream write', async () => {
  const baseURL = await startProvider(response => {
    response.setHeader('Content-Type', 'text/event-stream');
    response.write('data: {"choices":[{"delta":{"content":"first"}}]}\n\n');
    setTimeout(() => response.end(`data: ${'x'.repeat(5000)}\n\n`), 100);
  });
  const outcome = await query(baseURL).invoke(call(() => { throw new Error('text stream unavailable'); }), request());
  expect(outcome.kind).toBe('retry');
  expect(outcome.failure?.kind).toBe('AVAILABILITY');
});

it('allows an injected HTTP client to return the Go-compatible complete response', async () => {
  const format = newWireFormat({ providerName: 'chat' });
  let requests = 0;
  const providerQuery = query('https://private.example.test', format, config => {
    config.httpClient = { send: async requestData => {
      requests++;
      expect(requestData.addressPolicy).toBe('unrestricted');
      return { status: 200, headers: { 'content-type': 'application/json' }, body: Buffer.from('{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}') };
    } };
  });
  const outcome = await providerQuery.invoke(call(), request());
  expect(requests).toBe(1);
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') expect(outcome.branch).toBe('generated');
});

it('decodes a buffered event stream from an injected HTTP client', async () => {
  const format = newWireFormat({ providerName: 'chat', streaming: 'always' });
  const providerQuery = query('https://private.example.test', format, config => {
    config.httpClient = { send: async () => ({
      status: 200, headers: { 'content-type': 'text/event-stream' },
      body: Buffer.from('data: {"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n'),
    }) };
  });
  const outcome = await providerQuery.invoke(call(), request());
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') {
    expect(outcome.branch).toBe('generated');
    expect(outcome.value.text).toBe('ok');
  }
});

it('uses Retry-After when the provider retry-delay reader throws on an HTML 503', async () => {
  const format = newWireFormat({ providerName: 'chat' });
  format.readErrorRetryDelay = body => JSON.parse(Buffer.from(body).toString('utf8')) as number;
  const providerQuery = query('https://private.example.test', format, config => {
    config.httpClient = { send: async () => ({
      status: 503, headers: { 'retry-after': '7' }, body: Buffer.from('<html>unavailable</html>'),
    }) };
  });
  const outcome = await providerQuery.invoke(call(), request());
  expect(outcome.kind).toBe('retry');
  if (outcome.kind === 'retry') {
    expect(outcome.failure.kind).toBe('AVAILABILITY');
    expect(outcome.retryAfterMs).toBe(7000);
  }
});

it('keeps the copied wire-format finish mapping after caller mutation', async () => {
  const format = newWireFormat({ providerName: 'chat' });
  const providerQuery = query('https://private.example.test', format, config => {
    config.httpClient = { send: async () => ({ status: 200, headers: {},
      body: Buffer.from('{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}') }) };
  });
  format.finishReasons.stop = 'contentPolicy';
  format.requestIDHeaders?.push('x-invalid');
  const outcome = await providerQuery.invoke(call(), request());
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') expect(outcome.branch).toBe('generated');
});

it('rejects an unaccepted OpenAI Chat field before sending a request', async () => {
  let requests = 0;
  const baseURL = await startProvider(response => { requests++; response.end('{}'); });
  const format = newWireFormat({ providerName: 'chat', allowedRequestFields: ['model', 'messages'] });
  const outcome = await query(baseURL, format).invoke(call(), { ...request(), temperature: 0.5 });
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') expect(outcome.branch).toBe('defect');
  expect(requests).toBe(0);
});

it('counts every keep-alive byte toward stream stall progress', async () => {
  const baseURL = await startProvider(async response => {
    response.setHeader('Content-Type', 'text/event-stream');
    for (let index = 0; index < 4; index++) {
      response.write(': keep-alive\n\n');
      await new Promise<void>(resolve => setTimeout(resolve, 30));
    }
    response.end('data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n');
  });
  const format = newWireFormat({ providerName: 'chat', streaming: 'always', stallTimeoutMs: 50 });
  const outcome = await query(baseURL, format).invoke(call(), request());
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') expect(outcome.branch).toBe('generated');
});

it('sends declared Chat headers and records safe receipt headers', async () => {
  const format = newWireFormat({ providerName: 'chat', fixedHeaders: { 'X-Profile': 'fixture' },
    requestIDHeaders: ['x-request-id'], rateLimitHeaders: ['x-ratelimit-remaining'] });
  let observedHeaders: Record<string, string> | undefined;
  const providerQuery = query('https://private.example.test', format, config => {
    config.httpClient = { send: async providerRequest => {
      observedHeaders = providerRequest.headers;
      return { status: 200, headers: { 'x-request-id': 'req-123', 'x-ratelimit-remaining': '17',
        'x-private-message': 'never publish' },
      body: Buffer.from('{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}') };
    } };
  });
  const outcome = await providerQuery.invoke(call(), request());
  expect(observedHeaders?.['X-Profile']).toBe('fixture');
  expect(observedHeaders?.Authorization).toBe('Bearer test-only');
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') {
    expect(outcome.receipt.providerRequestId).toBe('req-123');
    expect(outcome.receipt.metadata).toEqual({ 'x-ratelimit-remaining': '17' });
  }
});

it('classifies an OpenAI Chat top-level 2xx error envelope without leaking its message', async () => {
  const baseURL = await startProvider(response => { response.setHeader('Content-Type', 'application/json');
    response.end('{"error":{"code":"blocked","message":"private provider explanation"}}'); });
  const format = newWireFormat({ providerName: 'chat', errorRules: [{ statusCode: 0, errorToken: 'blocked', outcome: blockedOutcome() }] });
  const outcome = await query(baseURL, format).invoke(call(), request());
  expect(outcome.kind).toBe('branch');
  if (outcome.kind === 'branch') expect(outcome.branch).toBe('blocked');
  expect(outcome.failure?.message).not.toContain('private provider explanation');
});
