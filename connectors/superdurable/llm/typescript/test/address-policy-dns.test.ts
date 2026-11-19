// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { afterAll, expect, it, vi } from 'vitest';
import dns from 'node:dns/promises';
import { syncBuiltinESMExports } from 'node:module';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest, AddressRefusal } from '@superdurable/connectors-sdk/providerhttp';
import { Client } from '../src/client.js';

const lookupAddresses = vi.fn();
const originalLookup = dns.lookup;
dns.lookup = lookupAddresses as typeof dns.lookup;
syncBuiltinESMExports();
afterAll(() => { dns.lookup = originalLookup; syncBuiltinESMExports(); });

const credentials = { resolve: async () => ({ apiKey: new SecretString('sk-inert-test') }) };

it.each(['255.255.255.255', '100.64.0.1', 'fd00:ec2::254', '::ffff:10.0.0.1'])(
  'refuses SDK-resolved DNS answer %s', async address => {
    lookupAddresses.mockReset().mockResolvedValue([{ address, family: address.includes(':') ? 6 : 4 }]);
    await expect(sendProviderRequest({
      url: 'https://bcast.example/v1/chat/completions', method: 'POST', body: Buffer.from('{}'),
      maxResponseBytes: 1024, addressPolicy: 'publicHTTPS',
    })).rejects.toBeInstanceOf(AddressRefusal);
    expect(lookupAddresses).toHaveBeenCalledWith('bcast.example', { all: true, verbatim: true });
  },
);

it('refuses every DNS answer when one is shared address space', async () => {
  lookupAddresses.mockReset().mockResolvedValue([
    { address: '8.8.8.8', family: 4 }, { address: '100.64.0.1', family: 4 },
  ]);
  await expect(sendProviderRequest({
    url: 'https://bcast.example/v1/chat/completions', method: 'POST', body: Buffer.from('{}'),
    maxResponseBytes: 1024, addressPolicy: 'publicHTTPS',
  })).rejects.toBeInstanceOf(AddressRefusal);
});

it('routes OpenAI-compatible requests through the SDK DNS policy', async () => {
  lookupAddresses.mockReset().mockResolvedValue([{ address: '255.255.255.255', family: 4 }]);
  const client = new Client({
    provider: 'openai-compatible', model: 'fixture-model', baseUrl: 'https://bcast.example/v1',
  }, credentials);
  const result = await client.generateText().invoke(testCall(), { messages: [{ role: 'user', text: 'Hello' }] });
  expect(result.kind).toBe('branch');
  expect(lookupAddresses).toHaveBeenCalledWith('bcast.example', { all: true, verbatim: true });
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
