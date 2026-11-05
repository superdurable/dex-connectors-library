// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MondayClient } from '../src/client.js';
import type { CredentialSource, Credentials } from '../src/generated/models.js';

function call(operation: string): Call {
  return {
    id: 'a5e14bae-f11c-5ec8-9d20-6d6f96e02b43', idempotencyKey: 'a5e14bae-f11c-5ec8-9d20-6d6f96e02b43', operation: { connectorId: 'monday', operationId: operation },
    connection: { provider: 'monday', name: 'monday-test' },
    context: { attempt: 1, cancellationSignal: new AbortController().signal, deadline: new Date(Date.now() + 30_000),
      hasLastHeartbeatValue: () => false, getLastHeartbeatValue: () => undefined, recordHeartbeat: async () => {} },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  } as unknown as Call;
}
function personalCredentials(): CredentialSource {
  const resolve = async (): Promise<Credentials> => ({ authMethodId: 'personal-api-token', apiToken: new SecretString('test-token') });
  return { resolve, resolveWithRefresh: resolve };
}

describe('monday.com input and provider classification', () => {
  it('rejects calendar rollover and a display-name email before dispatch', async () => {
    let sends = 0;
    const client = new MondayClient({}, personalCredentials(), { send: async () => { sends++; throw new Error('unexpected send'); } });
    const invalidDate = await client.updateItemColumnValues().invoke(call('updateItemColumnValues'), {
      boardId: '1234567890', itemId: '9876543210', columnValues: { date4: { type: 'date', date: '2026-02-30' } },
    });
    const invalidEmail = await client.createItem().invoke(call('createItem'), {
      boardId: '1234567890', itemName: 'Test', columnValues: { email: { type: 'email', email: 'Jane <jane@example.com>' } },
    });
    expect(invalidDate).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(invalidEmail).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(sends).toBe(0);
  });

  it('matches Go bare-address handling for international mailboxes and domain literals', async () => {
    let sends = 0;
    const client = new MondayClient({}, personalCredentials(), { send: async () => {
      sends++;
      return { status: 200, headers: {}, body: Buffer.from('{"data":{"create_item":{"id":"9876543210","name":"Test"}}}') };
    } });
    for (const email of ['é@example.com', 'jane@exämple.com', 'jane@[127.0.0.1]']) {
      expect(await client.createItem().invoke(call('createItem'), { boardId: '1234567890', itemName: 'Test', columnValues: { email: { type: 'email', email } } })).toMatchObject({ kind: 'branch', branch: 'created' });
    }
    expect(await client.createItem().invoke(call('createItem'), { boardId: '1234567890', itemName: 'Test', columnValues: { email: { type: 'email', email: 'jane@[IPv6:::1]' } } })).toMatchObject({ kind: 'branch', branch: 'defect' });
    expect(sends).toBe(3);
  });

  it('separates revoked authorization from a transient refresh failure', async () => {
    const rejected = new MondayClient({}, { resolve: async () => { throw new ReauthorizationRequiredError(); }, resolveWithRefresh: async () => { throw new ReauthorizationRequiredError(); } });
    const unavailable = new MondayClient({}, { resolve: async () => { throw new RetryError({ kind: 'AVAILABILITY', provider: 'monday', operation: 'getItem', message: 'refresh unavailable' }); }, resolveWithRefresh: async () => { throw new RetryError({ kind: 'AVAILABILITY', provider: 'monday', operation: 'getItem', message: 'refresh unavailable' }); } });
    expect(await rejected.getItem().invoke(call('getItem'), { itemId: '9876543210' })).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHENTICATION' } });
    expect(await unavailable.getItem().invoke(call('getItem'), { itemId: '9876543210' })).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  });

  it('retries a provably unsent request and classifies a large 503 by status', async () => {
    const unreachable = new MondayClient({}, personalCredentials(), { send: async () => { throw new ProviderTransportError(true); } });
    expect(await unreachable.getItem().invoke(call('getItem'), { itemId: '9876543210' })).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
    const unavailable = new MondayClient({ maxResponseBytes: 1 }, personalCredentials(), { send: async () => { throw new BodyTooLargeError(503, {}); } });
    expect(await unavailable.getItem().invoke(call('getItem'), { itemId: '9876543210' })).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  });

  it('rejects a wrong-typed item field as an invalid provider response', async () => {
    const client = new MondayClient({}, personalCredentials(), { send: async () => ({ status: 200, headers: {}, body: Buffer.from(JSON.stringify({ data: { items: [{ id: '9876543210', name: 42 }] } })) }) });
    const result = await client.getItem().invoke(call('getItem'), { itemId: '9876543210' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
  });
});
