// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Call } from '@superdurable/connectors-sdk';
import { newProjectConnection } from '../src/connection.js';
import { noProgress } from './call.js';

const call: Call = {
  ...noProgress,
  id: '11111111-1111-5111-8111-111111111111',
  context: { flowId: 'test-flow', cancellationSignal: new AbortController().signal } as Call['context'],
  connection: { provider: 'stripe', name: 'stripe-payments' },
  operation: { connectorId: 'stripe', operationId: 'getCheckoutSession' },
};

describe('project connection', () => {
  it('reads ordinary settings at construction and private credentials at invocation', async () => {
    let reads = 0;
    const project = {
      configuration: { connections: [{ connectorId: 'stripe', connectionName: 'stripe-payments', provider: 'stripe', configuration: { endpoint: 'http://localhost:12345/v1' } }] },
      resolveCredential: async () => { reads++; return { auth_method: 'api-key-only', secret_key: 'rk_test_private' }; },
    } as unknown as LoadedProject;
    const connection = newProjectConnection(project, 'stripe-payments', {
      send: async request => {
        expect(request.headers?.Authorization).toBe(`Basic ${Buffer.from('rk_test_private:').toString('base64')}`);
        return { status: 404, headers: {}, body: new Uint8Array() };
      },
    });
    expect(reads).toBe(0);
    expect(connection.reference.name).toBe('stripe-payments');
    expect(await connection.operations.getCheckoutSession().invoke(call, { sessionId: 'cs_123' })).toMatchObject({ kind: 'branch', branch: 'notFound' });
    expect(reads).toBe(1);
    expect(() => JSON.stringify(connection)).toThrow('cannot be serialized');
  });

  it('fails closed on a missing API key', async () => {
    const project = {
      configuration: { connections: [{ connectorId: 'stripe', connectionName: 'stripe-payments', provider: 'stripe', configuration: {} }] },
      resolveCredential: async () => ({ auth_method: 'api-key-with-webhook', secret_key: '' }),
    } as unknown as LoadedProject;
    const connection = newProjectConnection(project, 'stripe-payments');
    expect(await connection.operations.getCheckoutSession().invoke(call, { sessionId: 'cs_123' })).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
  });

  it.each([
    { name: 'API key only', fields: { auth_method: 'api-key-only', secret_key: 'rk_test_private' }, branch: 'notFound', requests: 1 },
    { name: 'webhook method without signing secret', fields: { auth_method: 'api-key-with-webhook', secret_key: 'rk_test_private' }, branch: 'notFound', requests: 1 },
    { name: 'webhook method with signing secret', fields: { auth_method: 'api-key-with-webhook', secret_key: 'rk_test_private', webhook_secret: 'whsec_private' }, branch: 'notFound', requests: 1 },
    { name: 'legacy credentials without a method or signing secret', fields: { secret_key: 'rk_test_private' }, branch: 'notFound', requests: 1 },
    { name: 'missing API key', fields: { auth_method: 'api-key-only', secret_key: '' }, branch: 'defect', requests: 0 },
    { name: 'unknown method', fields: { auth_method: 'unknown', secret_key: 'rk_test_private' }, branch: 'defect', requests: 0 },
  ])('validates $name at invocation without exposing credential fields', async ({ fields, branch, requests }) => {
    let sends = 0;
    const project = {
      configuration: { connections: [{ connectorId: 'stripe', connectionName: 'stripe-payments', provider: 'stripe', configuration: {} }] },
      resolveCredential: async () => fields,
    } as unknown as LoadedProject;
    const connection = newProjectConnection(project, 'stripe-payments', {
      send: async request => {
        sends++;
        expect(request.headers?.Authorization).toBe(`Basic ${Buffer.from('rk_test_private:').toString('base64')}`);
        expect(JSON.stringify(request)).not.toContain('whsec_private');
        return { status: 404, headers: {}, body: new Uint8Array() };
      },
    });
    const result = await connection.operations.getCheckoutSession().invoke(call, { sessionId: 'cs_123' });
    expect(result).toMatchObject({ kind: 'branch', branch });
    if (branch === 'defect') expect(result).toMatchObject({ failure: { kind: 'AUTHENTICATION' } });
    expect(sends).toBe(requests);
  });
});
