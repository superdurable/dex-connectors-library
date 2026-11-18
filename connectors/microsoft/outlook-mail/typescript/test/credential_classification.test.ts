// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call, type CredentialProvider, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError, RejectedCredentialNotRefreshedError } from '@superdurable/connectors-sdk/projectconfig';
import { OutlookMailClient } from '../src/client.js';
import { decodeCredentials, type Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const connection = { provider: 'microsoft', name: 'outlook-mail-test' };
const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false, refresh: async () => { throw new Error('unexpected refresh'); } };
const noRequest = async (): Promise<never> => { throw new Error('no request is expected'); };
const terminalErrors: Error[] = [new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError()];
const transientErrors: Error[] = [new CredentialBrokerUnavailableError(), new RejectedCredentialNotRefreshedError(), new Error('connection is not configured'), new TypeError('unlisted')];
const sendInput = { to: ['a@example.com'], subject: 'Test', text: 'Body' };

function failing(error: Error): CredentialProvider<Credentials> {
  return { resolve: async () => { throw error; } };
}

function sendCall(priorHeartbeat: unknown): Call {
  return scenarioCall({ callId: 'credential-call', operation: 'sendMessage' } as Scenario, connection, 'outlook-mail',
    new HeartbeatContext(priorHeartbeat), { idempotencyKey: 'credential-call' }) as Call;
}

it.each([...terminalErrors, ...transientErrors].map(error => [error.name, error] as const))('sendMessage classifies %s like every connector', async (_name, error) => {
  const isTerminal = terminalErrors.includes(error);
  const client = new OutlookMailClient({}, failing(error), { refreshDriver: driver, transport: noRequest });
  const first = await client.sendMessage().invoke(sendCall(undefined), sendInput);
  const dispatched = { value: { outlookMailSendPhase: 'sendDispatched', outlookMailDraftMessageId: 'draft1', outlookMailSendCount: 1 }, attempt: 1 };
  const later = await client.sendMessage().invoke(sendCall(dispatched), sendInput);
  if (isTerminal) {
    expect(first).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
    expect(later).toMatchObject({ kind: 'uncertain' });
  } else {
    expect(first).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(later).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  }
});

it('retries a static connection that is not configured', async () => {
  const credentials: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client-id', clientSecret: new SecretString('secret'),
    accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };
  const other = new StaticCredentialProvider([[{ provider: 'microsoft', name: 'other' }, credentials]]);
  const client = new OutlookMailClient({}, other, { refreshDriver: driver, transport: noRequest });
  expect(await client.sendMessage().invoke(sendCall(undefined), sendInput)).toMatchObject({ kind: 'retry' });
});

it('selects defect for malformed stored credentials before a send and uncertain after one', async () => {
  let wrapped: Error | undefined;
  try { decodeCredentials({ unexpected_field: 'value' }); }
  catch { wrapped = new TypeError('project credential decoding failed'); }
  const client = new OutlookMailClient({}, failing(wrapped!), { refreshDriver: driver, transport: noRequest });
  expect(await client.sendMessage().invoke(sendCall(undefined), sendInput)).toMatchObject({ kind: 'branch', branch: 'defect' });
  const dispatched = { value: { outlookMailSendPhase: 'sendDispatched', outlookMailDraftMessageId: 'draft1', outlookMailSendCount: 1 }, attempt: 1 };
  expect(await client.sendMessage().invoke(sendCall(dispatched), sendInput)).toMatchObject({ kind: 'uncertain' });
});
