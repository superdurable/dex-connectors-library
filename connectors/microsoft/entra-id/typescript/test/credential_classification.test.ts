// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError, RejectedCredentialNotRefreshedError } from '@superdurable/connectors-sdk/projectconfig';
import { EntraClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const terminalErrors: Error[] = [new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError()];
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

it('treats credential material that fails local checks as a defect', async () => {
  const unsafe: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
    accessToken: new SecretString('bad\ntoken'), refreshToken: new SecretString('refresh') };
  const client = clientResolving(async () => unsafe);
  expect(await client.addUserToGroup().invoke(entraCall('addUserToGroup'), { groupId: userId, userId })).toMatchObject({ kind: 'branch', branch: 'defect' });
});
