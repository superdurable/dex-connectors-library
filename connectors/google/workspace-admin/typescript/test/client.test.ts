// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, CredentialProvider, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { WorkspaceAdminClient, goPathEscape, goQueryEncode } from '../src/client.js';
import { CredentialMaterialError, decodeCredentials } from '../src/generated/models.js';

const credentials = { resolve: async () => ({ accessToken: new SecretString('token') }), resolveWithRefresh: async () => ({ accessToken: new SecretString('token') }) };

describe('Directory client inputs', () => {
  it('escapes paths and sorted queries like Go', () => {
    expect(goPathEscape("a!$&+,:;=@/?'()*~")).toBe('a%21$&+%2C:%3B=@%2F%3F%27%28%29%2A~');
    expect(goQueryEncode({ z: 'a b~*', a: 'x+y' })).toBe('a=x%2By&z=a+b~%2A');
  });
  it('rejects invalid limits before a provider call', () => {
    expect(() => new WorkspaceAdminClient({ maxResponseBytes: -1 }, credentials)).toThrow();
    expect(() => new WorkspaceAdminClient({ listUsersPageSize: 501 }, credentials)).toThrow();
  });
});

describe('credential resolve failures', () => {
  const call = {
    id: '3a0d2b5e-6b1f-5c8d-9e2a-7f4c1d0b9a12', connection: { provider: 'google', name: 'workspace' },
    operation: { connectorId: 'google-workspace-admin', operationId: 'getUser' },
    context: { cancellationSignal: new AbortController().signal },
  } as unknown as Call;

  it.each([
    { name: 'reauthorization', failure: new ReauthorizationRequiredError(), isTerminal: true },
    { name: 'object not found', failure: new ObjectNotFoundError(), isTerminal: true },
    { name: 'connection not selected', failure: new ConnectionNotSelectedError(), isTerminal: true },
    { name: 'broker rejected', failure: new CredentialBrokerRejectedError(), isTerminal: true },
    { name: 'malformed material', failure: new CredentialMaterialError('stored credential is malformed'), isTerminal: true },
    { name: 'broker unavailable', failure: new CredentialBrokerUnavailableError(), isTerminal: false },
    { name: 'static connection not configured', failure: new Error('connection is not configured'), isTerminal: false },
  ])('selects defect only for a terminal $name failure', async ({ failure, isTerminal }) => {
    const failing = { resolve: async () => { throw failure; }, resolveWithRefresh: async () => { throw failure; } };
    const client = new WorkspaceAdminClient({}, failing as never, { send: async () => { throw new Error('no request is expected'); } });
    const result = await client.getUser().invoke(call, { userKey: 'ada@example.com' });
    expect(result).toMatchObject(isTerminal ? { kind: 'branch', branch: 'defect' } : { kind: 'retry' });
  });

  it('selects defect for malformed stored material behind the released SDK provider', async () => {
    const store = {
      scope: { projectId: 'project-1', kind: 'live' },
      readCredentialMaterial: async () => ({ material: { credentials: { unexpected: 'value' } }, connection: {} }),
    };
    const provider = new CredentialProvider(store as never, { connectorId: 'google-workspace-admin', connectionName: 'workspace' }, decodeCredentials);
    const client = new WorkspaceAdminClient({}, provider as never, { send: async () => { throw new Error('no request is expected'); } });
    expect(await client.getUser().invoke(call, { userKey: 'ada@example.com' })).toMatchObject({ kind: 'branch', branch: 'defect' });
    const createCall = { ...call, operation: { connectorId: 'google-workspace-admin', operationId: 'createUser' }, idempotencyKey: 'hire-42' } as unknown as Call;
    expect(await client.createUser().invoke(createCall, { primaryEmail: 'ada@example.com', givenName: 'Ada', familyName: 'Lovelace' })).toMatchObject({ kind: 'branch', branch: 'defect' });
  });
});

describe('createUser after an earlier attempt', () => {
  const account = { id: '1001', primaryEmail: 'ada@example.com', externalIds: [{ type: 'custom', customType: 'dexIdempotencyKey', value: 'hire-42' }] };
  const input = { primaryEmail: 'ada@example.com', givenName: 'Ada', familyName: 'Lovelace' };
  const callAt = (attempt: number, firstAttemptAt = new Date()) => ({
    id: '3a0d2b5e-6b1f-5c8d-9e2a-7f4c1d0b9a13', connection: { provider: 'google', name: 'workspace' },
    operation: { connectorId: 'google-workspace-admin', operationId: 'createUser' }, idempotencyKey: 'hire-42',
    context: { cancellationSignal: new AbortController().signal, attempt, firstAttemptAt },
  }) as unknown as Call;
  const answering = (insertStatus: number, readStatus: number, methods: string[]) => new WorkspaceAdminClient({}, credentials, {
    send: async (request: { method: string }) => {
      methods.push(request.method);
      const isInsert = request.method === 'POST';
      const body = isInsert ? { error: { errors: [{ reason: 'forbidden' }] } } : readStatus === 200 ? account : {};
      return { status: isInsert ? insertStatus : readStatus, headers: {}, body: Buffer.from(JSON.stringify(body)) };
    },
  } as never);

  it('treats a first attempt refusal as definite', async () => {
    const methods: string[] = [];
    expect(await answering(403, 200, methods).createUser().invoke(callAt(1), input)).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
    expect(methods).toEqual(['POST']);
  });
  it('reads back a later refusal and finds this Step account', async () => {
    const methods: string[] = [];
    expect(await answering(403, 200, methods).createUser().invoke(callAt(2), input)).toMatchObject({ kind: 'branch', branch: 'created', value: { wasAlreadyCreated: true } });
    expect(methods).toEqual(['POST', 'GET']);
  });
  it('keeps waiting for an unreadable conflicting address after an earlier attempt', async () => {
    const late = new Date(Date.now() - 120_000);
    expect(await answering(409, 404, []).createUser().invoke(callAt(2, late), input)).toMatchObject({ kind: 'retry' });
  });
  it('retries an unlisted status', async () => {
    expect(await answering(412, 200, []).createUser().invoke(callAt(1), input)).toMatchObject({ kind: 'retry' });
  });
});
