// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError, RejectedCredentialNotRefreshedError } from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { OneDriveClient } from '../src/client.js';
import { CredentialMaterialError, decodeCredentials, type Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const connection = { provider: 'microsoft', name: 'onedrive-files' };
/** Reproduce the SDK refresh provider, which replaces the decoder error with this TypeError. */
function sdkDecodingFailure(): TypeError {
  try { decodeCredentials({ access_token: 7 }); } catch { return new TypeError('project credential decoding failed'); }
  throw new Error('malformed material decoded');
}
const terminalErrors: Error[] = [new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError(),
  new CredentialMaterialError('credential access_token is required'), sdkDecodingFailure()];
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

it.each(terminalErrors.map(error => [error.name, error] as const))('retries %s on a later write attempt, which may follow an applied write', async (_name, error) => {
  const client = new OneDriveClient({ endpoint: 'http://127.0.0.1:12345' }, { resolve: async () => { throw error; } }, {
    transport: { send: async () => { throw new Error('no request is expected'); } },
  });
  const first = oneDriveCall('createFolder');
  const later = { ...first, context: { ...first.context, attempt: 2 } } as Call;
  expect((await client.createFolder().invoke(later, { driveId: 'b!teamDriveKey-2', name: 'Dex copies' })).kind).toBe('retry');
});

it.each([
  ['a definite refusal', 403, '{"error":{"code":"accessDenied"}}', 'providerRejected'],
  ['a full quota', 507, '{"error":{"code":"quotaLimitReached"}}', 'providerRejected'],
  ['not implemented', 501, '{"error":{"code":"notSupported"}}', 'retry'],
  ['an unlisted status', 412, '{"error":{"code":"resourceModified"}}', 'retry'],
  ['another conflict', 409, '{"error":{"code":"otherConflict"}}', 'retry'],
] as const)('selects providerRejected for a write only after %s', async (_name, status, body, expected) => {
  const source = new StaticCredentialProvider([[connection, { accessToken: new SecretString('token') }]]);
  const client = new OneDriveClient({ endpoint: 'http://127.0.0.1:12345' }, source, {
    transport: { send: async () => ({ status, headers: {}, body: Buffer.from(body) }) },
  });
  const folder = await client.createFolder().invoke(oneDriveCall('createFolder'), { driveId: 'b!teamDriveKey-2', name: 'Dex copies' });
  const upload = await client.uploadFile().invoke(oneDriveCall('uploadFile'), { driveId: 'b!teamDriveKey-2', name: 'a.txt', conflictBehavior: 'replace', mimeType: 'text/plain', textContent: 'x' });
  for (const result of [folder, upload]) expect(result.kind === 'branch' ? result.branch : result.kind).toBe(expected);
});

it('repeats a write whose refusal body is oversized', async () => {
  const source = new StaticCredentialProvider([[connection, { accessToken: new SecretString('token') }]]);
  const client = new OneDriveClient({ endpoint: 'http://127.0.0.1:12345' }, source, {
    transport: { send: async () => { throw new BodyTooLargeError(403, {}); } },
  });
  expect((await client.createFolder().invoke(oneDriveCall('createFolder'), { driveId: 'b!teamDriveKey-2', name: 'Dex copies' })).kind).toBe('retry');
});

it('treats invalid material as a defect and an unconfigured static connection as transient', async () => {
  const unsafe = new StaticCredentialProvider([[connection, { accessToken: new SecretString('bad\ntoken') }]]);
  for (const result of await outcomes(unsafe)) expect(result).toMatchObject({ kind: 'branch', branch: 'defect' });
  const unconfigured = new StaticCredentialProvider([[{ provider: 'microsoft', name: 'other' }, { accessToken: new SecretString('token') }]]);
  for (const result of await outcomes(unconfigured)) expect(result).toMatchObject({ kind: 'retry' });
});
