// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it, vi } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString } from '@superdurable/connectors-sdk';
import { Client } from '../src/client.js';
import { isAllowedOpenAICompatibleAddress } from '../src/address-policy.js';
import { newChatWireFormat } from '../src/chat-profiles.js';
import { newResponsesWireFormat } from '../src/responses-wire-format.js';

const { sendRequest } = vi.hoisted(() => ({ sendRequest: vi.fn() }));
vi.mock('@superdurable/connectors-sdk/providerhttp', async importOriginal => ({
  ...await importOriginal<typeof import('@superdurable/connectors-sdk/providerhttp')>(),
  sendProviderRequest: sendRequest,
}));

const credentials = { resolve: async () => ({ apiKey: new SecretString('sk-inert-test') }) };

it.each([
  ['127.0.0.1', true, true], ['127.5.4.3', true, true], ['::1', true, true],
  ['8.8.8.8', false, true], ['2001:4860:4860::8888', false, true],
  ['0.0.0.0', false, false], ['0.1.2.3', false, true], ['10.0.0.1', false, false],
  ['100.64.0.1', false, false], ['169.254.169.254', false, false],
  ['172.16.0.1', false, false], ['192.168.1.1', false, false],
  ['224.0.0.1', false, false], ['255.255.255.255', false, false],
  ['::', false, false], ['::1', false, false], ['fc00::1', false, false],
  ['fe80::1', false, false], ['ff00::1', false, false], ['fd00:ec2::254', false, false],
  ['::ffff:7f00:1', true, true], ['::ffff:a00:1', false, false],
] as const)('matches Go address policy for %s', (address, loopback, allowed) => {
  expect(isAllowedOpenAICompatibleAddress(address, loopback)).toBe(allowed);
});

it('rejects private literal custom endpoints before credentials can be used', () => {
  expect(() => new Client({ provider: 'openai-compatible', model: 'fixture-model', baseUrl: 'https://10.0.0.1/v1' }, credentials)).toThrow(/host must be public/);
  expect(() => new Client({ provider: 'openai-compatible', model: 'fixture-model', baseUrl: 'http://127.0.0.1:34120/v1' }, credentials)).not.toThrow();
});

it.each(['openai', 'openai-compatible', 'anthropic', 'anthropic-compatible', 'gemini', 'qwen', 'deepseek', 'meta', 'mistral', 'kimi', 'xai'])(
  'constructs the %s dialect with a default model or required custom model', provider => {
    const config = provider === 'openai-compatible'
      ? { provider, model: 'fixture-model', baseUrl: 'http://127.0.0.1:34120/v1' }
      : provider === 'anthropic-compatible' ? { provider, model: 'fixture-model', baseUrl: 'http://127.0.0.1:34120' }
      : { provider };
    const client = new Client(config, credentials);
    expect(client.generateText().definition().operation.operationId).toBe('generateText');
  },
);

it('keeps wrong-typed OpenAI response data invalid', () => {
  const format = newResponsesWireFormat();
  const response = Buffer.from(JSON.stringify({
    id: 42, model: 'gpt-6-sol', status: 'completed',
    output: [{ type: 'message', content: [{ type: 'output_text', text: 'answer' }] }],
  }));
  expect(() => format.decodeResponse?.(response)).toThrow(/string field/);
});

it('keeps provider-specific reasoning constraints', () => {
  expect(newChatWireFormat('deepseek').rulesForModel('deepseek-flash').temperature.kind).toBe('notAccepted');
  expect(newChatWireFormat('qwen').rulesForModel('qwen3.8-max').reasoningEfforts.none).toBe('none');
  expect(newChatWireFormat('xai').rulesForModel('grok-4.20').reasoningEfforts).toEqual({});
});


it.each(['openai-compatible', 'openai', 'anthropic'])(
  'keeps the HTTPS host and applies public HTTPS policy for %s', async provider => {
  sendRequest.mockReset().mockResolvedValue({ status: 503, headers: {}, body: Buffer.alloc(0) });
  const config = provider === 'openai-compatible'
    ? { provider, model: 'fixture-model', baseUrl: 'https://bcast.example/v1' } : { provider };
  const client = new Client(config, credentials);
  await client.generateText().invoke(testCall(), { messages: [{ role: 'user', text: 'Hello' }] });
  const request = sendRequest.mock.calls[0]?.[0] as ProviderRequest;
  expect(new URL(request.url).hostname).toBe(provider === 'openai-compatible' ? 'bcast.example' :
    provider === 'openai' ? 'api.openai.com' : 'api.anthropic.com');
  expect(request.headers?.Host).toBeUndefined();
  expect(request.addressPolicy).toBe('publicHTTPS');
  },
);

it.each([
  [{ provider: 'openai-compatible', model: 'gateway-model', baseUrl: 'https://gateway.example/v1' }, '/v1/chat/completions', 'openai-compatible'],
  [{ provider: 'openai-compatible', model: 'gateway-model', baseUrl: 'https://gateway.example/v1', openaiCompatibleApi: 'chat-completions' },
    '/v1/chat/completions', 'openai-compatible'],
  [{ provider: 'openai-compatible', model: 'gateway-model', baseUrl: 'https://gateway.example/v1', openaiCompatibleApi: 'responses' },
    '/v1/responses', 'openai-compatible'],
  [{ provider: 'anthropic-compatible', model: 'gateway-model', baseUrl: 'https://gateway.example/anthropic' },
    '/anthropic/v1/messages', 'anthropic-compatible'],
])('sends a custom endpoint request to its protocol path %#', async (config, path, providerName) => {
  sendRequest.mockReset().mockResolvedValue({ status: 503, headers: {}, body: Buffer.alloc(0) });
  const client = new Client(config, credentials);
  const result = await client.generateText().invoke(testCall(), { messages: [{ role: 'user', text: 'Hello' }] });
  const request = sendRequest.mock.calls[0]?.[0] as ProviderRequest;
  expect(new URL(request.url).hostname).toBe('gateway.example');
  expect(new URL(request.url).pathname).toBe(path);
  expect(request.addressPolicy).toBe('publicHTTPS');
  expect(JSON.stringify(result)).toContain(providerName);
});

it('sends an Anthropic gateway only portable Messages fields', async () => {
  sendRequest.mockReset().mockResolvedValue({ status: 503, headers: {}, body: Buffer.alloc(0) });
  const client = new Client({ provider: 'anthropic-compatible', model: 'gateway-model', baseUrl: 'https://gateway.example/anthropic' }, credentials);
  const structured = await client.generateText().invoke(testCall(), {
    messages: [{ role: 'user', text: 'Reply with yes.' }],
    structuredOutput: { name: 'answer', schema: {
      type: 'object', properties: { answer: { type: 'string' } }, required: ['answer'], additionalProperties: false,
    } },
  });
  expect(structured.kind).toBe('retry');
  const request = sendRequest.mock.calls[0]?.[0] as ProviderRequest;
  const body = JSON.parse(Buffer.from(request.body as Uint8Array).toString('utf8')) as Record<string, unknown>;
  expect(body.output_config).toBeUndefined();
  expect(String(body.system)).toContain('"answer"');
  expect(request.headers?.['anthropic-version']).toBe('2023-06-01');
  const effort = await client.generateText().invoke(testCall(), { messages: [{ role: 'user', text: 'Hello' }], reasoningEffort: 'low' });
  expect(effort).toMatchObject({ kind: 'branch', branch: 'defect' });
});

it.each([
  [{ provider: 'anthropic-compatible', baseUrl: 'https://gateway.example/anthropic' }, /model is required/],
  [{ provider: 'anthropic-compatible', model: 'm', baseUrl: '' }, /baseUrl is required/],
  [{ provider: 'anthropic-compatible', model: 'm', baseUrl: 'https://gateway.example/v1' }, /must not end in \/v1/],
  [{ provider: 'anthropic-compatible', model: 'm', baseUrl: 'https://gateway.example/anthropic/v1/messages' }, /must not end in \/v1 or \/messages/],
  [{ provider: 'anthropic-compatible', model: 'm', baseUrl: 'https://10.0.0.1/anthropic' }, /public or loopback/],
  [{ provider: 'anthropic', baseUrl: 'https://gateway.example/anthropic' }, /baseUrl applies only to openai-compatible or anthropic-compatible/],
  [{ provider: 'openai', openaiCompatibleApi: 'responses' }, /openaiCompatibleApi applies only/],
  [{ provider: 'openai', openaiCompatibleApi: 'assistants' }, /openaiCompatibleApi is invalid/],
])('rejects invalid custom endpoint configuration %#', (config, message) => {
  expect(() => new Client(config, credentials)).toThrow(message);
});

it.each([
  [['gpt-6-sol', 'gpt-6-luna'], [], undefined],
  [[' gpt-6-sol ', 'a=b'], ['gpt-6-sol=128000', 'a=b= 100000000 '], undefined],
  [['gpt-6-sol', ' '], [], /models entry 2/],
  [['gpt-6-sol', ' gpt-6-sol'], [], /entry 2 repeats/],
  [Array.from({ length: 101 }, (_, index) => `model-${index}`), [], /more than 100 entries/],
  [['gpt-6-luna'], [], /must be one of the models entries/],
  [[], ['gpt-6-sol=1000'], /names a model that models does not list/],
  [['gpt-6-sol'], ['gpt-6-sol'], /<model ID>=<tokens>/],
  [['gpt-6-sol'], ['gpt-6-sol=0'], /from 1 to 100000000/],
  [['gpt-6-sol'], ['gpt-6-sol=100000001'], /from 1 to 100000000/],
  [['gpt-6-sol'], ['gpt-6-sol=1.5'], /from 1 to 100000000/],
  [['gpt-6-sol'], ['gpt-6-sol=1000', 'gpt-6-sol=2000'], /entry 2 repeats/],
])('applies the Go models rules %#', (models, modelContextWindowTokens, message) => {
  const construct = () => new Client({ provider: 'openai', models, modelContextWindowTokens }, credentials);
  if (!message) {
    expect(construct).not.toThrow();
    return;
  }
  expect(construct).toThrow(message);
  try { construct(); } catch (error) { expect(String(error)).not.toContain('gpt-6-luna'); }
});

it('rejects custom transports for every provider', () => {
  for (const config of [{ provider: 'openai' },
    { provider: 'openai-compatible', model: 'fixture-model', baseUrl: 'https://bcast.example/v1' }]) {
    expect(() => new Client(config, credentials, { httpClient: { send: sendRequest } })).toThrow(/built-in HTTP transport/);
  }
});

function testCall(): Call {
  return {
    id: 'llm-address-policy-test', connection: { provider: 'llm', name: 'llm-address-policy-test' },
    operation: { connectorId: 'llm', operationId: 'generateText' },
    context: { flowId: 'llm-address-policy-test', stepExecutionId: 'llm-address-policy-test', attempt: 1,
      cancellationSignal: new AbortController().signal, recordHeartbeat: async () => {} },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  } as unknown as Call;
}
