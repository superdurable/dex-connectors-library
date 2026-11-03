// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { newProjectConnection } from '../src/index.js';

const call = {
  id: 'test-call', connection: { provider: 'cloudflare', name: 'cloudflare' },
  context: { flowId: 'test-flow', cancellationSignal: new AbortController().signal },
} as Call;

it('decodes Dex Web timeout and resolves the current token at request time', async () => {
  const observed: ProviderRequest[] = [];
  const project = {
    configuration: { connections: [{ connectorId: 'cloudflare', provider: 'cloudflare', connectionName: 'cloudflare', configuration: {
      baseUrl: 'http://127.0.0.1:21201', timeout: 15_000_000_000, maxResponseBytes: 1024,
    } }] },
    async resolveCredential() { return { api_token: 'test-apitoken' }; },
  } as unknown as LoadedProject;
  const connection = newProjectConnection(project, 'cloudflare', {
    send: async request => {
      observed.push(request);
      return { status: 200, headers: {}, body: Buffer.from('{"success":true,"result":[]}') };
    },
  });
  const result = await connection.operations.listZones().invoke(call, {});
  expect(result.kind).toBe('branch');
  expect(observed[0]?.timeoutMs).toBe(15_000);
  expect(observed[0]?.headers?.Authorization).toBe('Bearer test-apitoken');
});

it('rejects an invalid duration before Worker registration', () => {
  const project = {
    configuration: { connections: [{ connectorId: 'cloudflare', provider: 'cloudflare', connectionName: 'cloudflare', configuration: { timeout: 'bad' } }] },
  } as unknown as LoadedProject;
  expect(() => newProjectConnection(project, 'cloudflare')).toThrow('positive Go duration');
});
