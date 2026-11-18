// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError, RejectedCredentialNotRefreshedError } from '@superdurable/connectors-sdk/projectconfig';
import { Client } from '../src/client.js';
import type { CredentialSource, Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const connection = { provider: 'microsoft', name: 'outlook-calendar-scheduler' };
const terminalErrors: Error[] = [new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError()];
const transientErrors: Error[] = [new CredentialBrokerUnavailableError(), new RejectedCredentialNotRefreshedError(), new Error('connection is not configured'), new TypeError('unlisted')];
const credentials: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
  accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };

function calendarCall(operation: string): Call {
  return scenarioCall({ callId: `calendar-${operation}`, operation } as Scenario, connection, 'outlook-calendar', new HeartbeatContext(undefined)) as Call;
}

async function outcomes(source: CredentialSource): Promise<unknown[]> {
  const client = new Client({}, source, {
    transport: async () => { throw new Error('no request is expected'); },
    baseURL: 'http://127.0.0.1:12345/v1.0',
  });
  return [
    await client.getEvent().invoke(calendarCall('getEvent'), { eventId: 'event1' }),
    await client.updateEvent().invoke(calendarCall('updateEvent'), { eventId: 'event1', subject: 'Renamed' }),
  ];
}

it.each([...terminalErrors, ...transientErrors].map(error => [error.name, error] as const))('classifies %s the same way for every operation', async (_name, error) => {
  const expected = terminalErrors.includes(error) ? { kind: 'branch', branch: 'defect' } : { kind: 'retry', failure: { kind: 'AVAILABILITY' } };
  const results = await outcomes({ resolve: async () => { throw error; } } as unknown as CredentialSource);
  for (const result of results) expect(result).toMatchObject(expected);
});

it('treats invalid material as a defect and an unconfigured static connection as transient', async () => {
  const unsafe = new StaticCredentialProvider([[connection, { ...credentials, accessToken: new SecretString('bad\ntoken') }]]);
  for (const result of await outcomes(unsafe as unknown as CredentialSource)) expect(result).toMatchObject({ kind: 'branch', branch: 'defect' });
  const unconfigured = new StaticCredentialProvider([[{ provider: 'microsoft', name: 'other' }, credentials]]);
  for (const result of await outcomes(unconfigured as unknown as CredentialSource)) expect(result).toMatchObject({ kind: 'retry' });
});
