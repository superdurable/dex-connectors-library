// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FigmaClient } from '../src/client.js';

const call = {
  id: 'deb1de08-84b3-554e-91d5-e46e685d1f56',
  connection: { provider: 'figma', name: 'figma' },
  operation: { connectorId: 'figma', operationId: 'getFile' },
  context: { cancellationSignal: new AbortController().signal },
} as Call;

it('rejects invalid input and unavailable credentials before sending a request', async () => {
  let requests = 0;
  const client = new FigmaClient({}, { resolve: async () => { throw new Error('private credential failure'); } }, {
    send: async () => { requests++; throw new Error('unexpected request'); },
  });
  const invalid = await client.getFile().invoke(call, { fileKey: 'bad/key' });
  const unavailable = await client.getFile().invoke(call, { fileKey: 'fileA' });
  expect(invalid).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
  expect(unavailable).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
  expect(JSON.stringify(unavailable)).not.toContain('private credential failure');
  expect(requests).toBe(0);
});

it('retries a refused provider connection and keeps the token out of the failure', async () => {
  const client = new FigmaClient({}, { resolve: async () => ({ apiKey: new SecretString('secret-value') }) }, {
    send: async request => {
      expect(request.url).toBe('https://api.figma.com/v1/files/fileA');
      expect(request.headers?.['X-Figma-Token']).toBe('secret-value');
      throw new ProviderTransportError(false);
    },
  });
  const result = await client.getFile().invoke(call, { fileKey: 'fileA' });
  expect(result).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  expect(JSON.stringify(result)).not.toContain('secret-value');
});
