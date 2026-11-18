// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, CredentialProvider, ObjectNotFoundError, RejectedCredentialNotRefreshedError } from '@superdurable/connectors-sdk/projectconfig';
import { EntraClient } from '../src/client.js';
import { CredentialMaterialError, decodeCredentials, type Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const terminalErrors: Error[] = [new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError(), new CredentialMaterialError('stored credential is malformed')];
const transientErrors: Error[] = [new CredentialBrokerUnavailableError(), new RejectedCredentialNotRefreshedError(), new Error('connection is not configured'), new TypeError('unlisted')];
const userId = '6e7b768e-07e2-4810-8459-485f84f8f204';

function entraCall(operation: string): Call {
  return scenarioCall({ callId: `entra-${operation}`, operation } as Scenario, { provider: 'microsoft', name: 'entra-id' },
    'microsoft-entra-id', new HeartbeatContext(undefined)) as Call;
}

function clientResolving(resolve: () => Promise<Credentials>): EntraClient {
  return new EntraClient({}, { resolve, resolveWithRefresh: resolve }, { send: async () => { throw new Error('no request is expected'); } });
}

it.each([...terminalErrors, ...transientErrors].map(error => [error.name, error] as const))('classifies %s the same way for every operation', async (_name, error) => {
  const client = clientResolving(async () => { throw error; });
  const expected = terminalErrors.includes(error) ? { kind: 'branch', branch: 'defect' } : { kind: 'retry', failure: { kind: 'AVAILABILITY' } };
  expect(await client.getUser().invoke(entraCall('getUser'), { userKey: userId })).toMatchObject(expected);
  expect(await client.addUserToGroup().invoke(entraCall('addUserToGroup'), { groupId: userId, userId })).toMatchObject(expected);
});

it('selects defect for malformed stored material behind the released SDK provider', async () => {
  const store = {
    scope: { projectId: 'project-1', kind: 'live' },
    readCredentialMaterial: async () => ({ material: { credentials: { unexpected: 'value' } }, connection: {} }),
  };
  const provider = new CredentialProvider(store as never, { connectorId: 'microsoft-entra-id', connectionName: 'entra-id' }, decodeCredentials);
  const client = new EntraClient({}, provider as never, { send: async () => { throw new Error('no request is expected'); } });
  expect(await client.addUserToGroup().invoke(entraCall('addUserToGroup'), { groupId: userId, userId })).toMatchObject({ kind: 'branch', branch: 'defect' });
});

it.each([
  { attempt: 1, expected: { kind: 'branch', branch: 'providerRejected' }, requests: ['POST'] },
  { attempt: 2, expected: { kind: 'branch', branch: 'created', value: { wasAlreadyCreated: true } }, requests: ['POST', 'GET'] },
])('reads back a createUser refusal only after an earlier attempt ($attempt)', async ({ attempt, expected, requests }) => {
  const methods: string[] = [];
  const account = { id: userId, userPrincipalName: 'ada.lovelace@contoso.com', accountEnabled: true, onPremisesExtensionAttributes: { extensionAttribute15: 'dex-creation-key:hire-42' } };
  const send = async (request: { method: string }) => {
    methods.push(request.method);
    return request.method === 'POST'
      ? { status: 403, headers: {}, body: Buffer.from('{"error":{"code":"Authorization_RequestDenied"}}') }
      : { status: 200, headers: {}, body: Buffer.from(JSON.stringify(account)) };
  };
  const token: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'), accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };
  const client = new EntraClient({}, { resolve: async () => token, resolveWithRefresh: async () => token }, { send } as never);
  const call = entraCall('createUser');
  const attemptCall = { ...call, idempotencyKey: 'hire-42', context: Object.assign(Object.create(Object.getPrototypeOf(call.context)), call.context, { attempt }) } as Call;
  const input = { userPrincipalName: 'ada.lovelace@contoso.com', givenName: 'Ada', surname: 'Lovelace', provisioningKey: 'hire-42' };
  expect(await client.createUser().invoke(attemptCall, input)).toMatchObject(expected);
  expect(methods).toEqual(requests);
});

it('treats credential material that fails local checks as a defect', async () => {
  const unsafe: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
    accessToken: new SecretString('bad\ntoken'), refreshToken: new SecretString('refresh') };
  const client = clientResolving(async () => unsafe);
  expect(await client.addUserToGroup().invoke(entraCall('addUserToGroup'), { groupId: userId, userId })).toMatchObject({ kind: 'branch', branch: 'defect' });
});
