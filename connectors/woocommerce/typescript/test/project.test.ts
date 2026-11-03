// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'woocommerce', connectionName: 'woocommerce', provider: 'woocommerce', configuration }] },
    resolveCredential: async () => ({ consumer_key: 'ck_test', consumer_secret: 'cs_test' }),
  } as unknown as LoadedProject;
}

describe('WooCommerce project connection', () => {
  it('rejects unknown and malformed settings', () => {
    expect(() => newProjectConnection(project({ storeUrl: 'http://localhost/shop', timeout: true }), 'woocommerce')).toThrow();
    expect(() => newProjectConnection(project({ storeUrl: 'http://localhost/shop', timeout: 'bogus' }), 'woocommerce')).toThrow();
    expect(() => newProjectConnection(project({ storeUrl: 'http://localhost/shop', other: 1 }), 'woocommerce')).toThrow();
  });

  it('maps the manifest timeout into the provider request', async () => {
    let timeoutMs = 0;
    const connection = newProjectConnection(project({ storeUrl: 'http://localhost/shop', timeout: '2s' }), 'woocommerce', {
      send: async request => { timeoutMs = request.timeoutMs ?? 0; return { status: 200, headers: {}, body: Buffer.from('[]') }; },
    });
    const call: Call = {
      id: 'project-test', connection: connection.reference, operation: { connectorId: 'woocommerce', operationId: 'listOrders' },
      context: { flowId: 'project-test', cancellationSignal: new AbortController().signal } as Call['context'],
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    await connection.operations.listOrders().invoke(call, { page: 1 });
    expect(timeoutMs).toBe(2_000);
  });
});
