// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError, RejectedCredentialNotRefreshedError } from '@superdurable/connectors-sdk/projectconfig';
import { OneDriveClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const connection = { provider: 'microsoft', name: 'onedrive-files' };
const terminalErrors: Error[] = [new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError()];
const transientErrors: Error[] = [new CredentialBrokerUnavailableError(), new RejectedCredentialNotRefreshedError(), new Error('connection is not configured'), new TypeError('unlisted')];

function oneDriveCall(operation: string): Call {
  return scenarioCall({ callId: `onedrive-${operation}`, operation } as Scenario, connection, 'microsoft-onedrive', new HeartbeatContext(undefined)) as Call;
}

async function outcomes(source: CredentialProvider<Credentials>): Promise<unknown[]> {
  const client = new OneDriveClient({ endpoint: 'http://127.0.0.1:12345' }, source, {
    transport: { send: async () => { throw new Error('no request is expected'); } },
  });
  return [
    await client.getFile().invoke(oneDriveCall('getFile'), { driveId: 'b!teamDriveKey-2', itemId: 'file-1' }),
    await client.createFolder().invoke(oneDriveCall('createFolder'), { driveId: 'b!teamDriveKey-2', name: 'Dex copies' }),
  ];
}

it.each([...terminalErrors, ...transientErrors].map(error => [error.name, error] as const))('classifies %s the same way for every operation', async (_name, error) => {
  const expected = terminalErrors.includes(error) ? { kind: 'branch', branch: 'defect' } : { kind: 'retry', failure: { kind: 'AVAILABILITY' } };
  const results = await outcomes({ resolve: async () => { throw error; } });
  for (const result of results) expect(result).toMatchObject(expected);
});

it('treats invalid material as a defect and an unconfigured static connection as transient', async () => {
  const unsafe = new StaticCredentialProvider([[connection, { accessToken: new SecretString('bad\ntoken') }]]);
  for (const result of await outcomes(unsafe)) expect(result).toMatchObject({ kind: 'branch', branch: 'defect' });
  const unconfigured = new StaticCredentialProvider([[{ provider: 'microsoft', name: 'other' }, { accessToken: new SecretString('token') }]]);
  for (const result of await outcomes(unconfigured)) expect(result).toMatchObject({ kind: 'retry' });
});
