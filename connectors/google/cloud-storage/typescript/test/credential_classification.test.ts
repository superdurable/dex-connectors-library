// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, CredentialProvider, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { CloudStorageClient } from '../src/client.js';
import { CredentialMaterialError, decodeCredentials, type Credentials } from '../src/generated/models.js';

const call = {
  id: '6f1d6c2e-3c1f-5b8e-9a43-2a7d5d1f4c10', connection: { provider: 'google-cloud-storage', name: 'google-cloud-storage' },
  operation: { connectorId: 'google-cloud-storage', operationId: 'uploadObject' },
  context: { cancellationSignal: new AbortController().signal },
} as unknown as Call;

it.each([
  { name: 'reauthorization', failure: new ReauthorizationRequiredError(), expected: 'credential' },
  { name: 'object not found', failure: new ObjectNotFoundError(), expected: 'credential' },
  { name: 'connection not selected', failure: new ConnectionNotSelectedError(), expected: 'credential' },
  { name: 'broker rejected', failure: new CredentialBrokerRejectedError(), expected: 'credential' },
  { name: 'malformed material', failure: new CredentialMaterialError('stored credential is malformed'), expected: 'credential' },
  { name: 'broker unavailable', failure: new CredentialBrokerUnavailableError(), expected: 'refresh' },
  { name: 'unlisted', failure: new Error('unlisted'), expected: 'refresh' },
])('classifies a $name resolve failure', async ({ failure, expected }) => {
  const client = new CloudStorageClient({}, { resolve: async () => { throw failure; } }, { send: async () => { throw new Error('no request is expected'); } });
  expect(await client.resolveToken(call)).toEqual({ token: '', error: expected });
});

it('selects defect for malformed stored material behind the released SDK provider', async () => {
  const store = {
    scope: { projectId: 'project-1', kind: 'live' },
    readCredentialMaterial: async () => ({ material: { credentials: { unexpected: 'value' } }, connection: {} }),
  };
  const provider = new CredentialProvider(store as never, { connectorId: 'google-cloud-storage', connectionName: 'google-cloud-storage' }, decodeCredentials);
  const client = new CloudStorageClient({}, provider as never, { send: async () => { throw new Error('no request is expected'); } });
  expect(await client.resolveToken(call)).toEqual({ token: '', error: 'credential' });
  const result = await client.uploadObject().invoke(call, { bucket: 'example-bucket', name: 'a', contentBase64: 'aGVsbG8=', contentType: 'text/plain' });
  expect(result).toMatchObject({ kind: 'branch', branch: 'defect' });
});

it.each([
  { attempt: 1, expected: { kind: 'branch', branch: 'providerRejected' } },
  { attempt: 2, expected: { kind: 'retry' } },
])('refuses an unreadable existing object only on a first attempt ($attempt)', async ({ attempt, expected }) => {
  const send = async (request: { method: string; url: string }) => {
    if (request.method === 'POST') return { status: 412, headers: {}, body: new Uint8Array() };
    if (request.url.includes('alt=media')) return { status: 403, headers: {}, body: new Uint8Array() };
    return { status: 200, headers: {}, body: Buffer.from('{"name":"a","generation":"123"}') };
  };
  const credentials = new StaticCredentialProvider<Credentials>([[call.connection, { accessToken: new SecretString('token') } as Credentials]]);
  const client = new CloudStorageClient({}, credentials, { send } as never);
  const attemptCall = { ...call, context: { ...call.context, attempt } } as unknown as Call;
  const result = await client.uploadObject().invoke(attemptCall, { bucket: 'example-bucket', name: 'a', contentBase64: 'aGVsbG8=', contentType: 'text/plain' });
  expect(result).toMatchObject(expected);
});

it('treats an unconfigured static connection as transient', async () => {
  const client = new CloudStorageClient({}, new StaticCredentialProvider<Credentials>([]), { send: async () => { throw new Error('no request is expected'); } });
  expect(await client.resolveToken(call)).toEqual({ token: '', error: 'refresh' });
});
