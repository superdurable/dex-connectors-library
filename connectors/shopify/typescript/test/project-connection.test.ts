// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

it('maps every Shopify project setting and resolves the token at invocation', async () => {
  let credentialReads = 0;
  const project = {
    configuration: { connections: [{
      connectorId: 'shopify', connectionName: 'shopify-orders', provider: 'shopify',
      configuration: { shopSubdomain: 'test-shop', timeout: '2s', maxResponseBytes: 512 },
    }] },
    resolveCredential: async () => { credentialReads++; return { access_token: 'test-token' }; },
  } as unknown as LoadedProject;
  const connection = newProjectConnection(project, 'shopify-orders', {
    send: async request => {
      expect(request.timeoutMs).toBe(2000);
      expect(request.maxResponseBytes).toBe(512);
      expect(request.headers?.['X-Shopify-Access-Token']).toBe('test-token');
      return { status: 404, headers: {}, body: new Uint8Array() };
    },
  });
  const call = { context: { cancellationSignal: new AbortController().signal } } as Call;
  expect(credentialReads).toBe(0);
  expect(await connection.operations.getOrder().invoke(call, { orderId: 101 })).toMatchObject({ kind: 'branch', branch: 'notFound' });
  expect(credentialReads).toBe(1);
});
