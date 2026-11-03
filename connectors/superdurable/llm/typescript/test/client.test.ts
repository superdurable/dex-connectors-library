// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString } from '@superdurable/connectors-sdk';
import { Client } from '../src/client.js';
import { isAllowedOpenAICompatibleAddress } from '../src/address-policy.js';
import { newChatWireFormat } from '../src/chat-profiles.js';
import { newResponsesWireFormat } from '../src/responses-wire-format.js';

const credentials = { resolve: async () => ({ apiKey: new SecretString('sk-inert-test') }) };

it.each([
  ['127.0.0.1', true, true], ['127.5.4.3', true, true], ['::1', true, true],
  ['8.8.8.8', false, true], ['2001:4860:4860::8888', false, true],
  ['0.0.0.0', false, false], ['10.0.0.1', false, false],
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

it.each(['openai', 'openai-compatible', 'anthropic', 'gemini', 'qwen', 'deepseek', 'meta', 'mistral', 'kimi', 'xai'])(
  'constructs the %s dialect with a default model or required custom model', provider => {
    const config = provider === 'openai-compatible'
      ? { provider, model: 'fixture-model', baseUrl: 'http://127.0.0.1:34120/v1' }
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
