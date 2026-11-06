// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { OutlookMailClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const connection = { provider: 'microsoft', name: 'outlook-mail-test' };
const credentials: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client-id', clientSecret: new SecretString('secret'),
  accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };
const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false, refresh: async () => { throw new Error('unexpected refresh'); } };
const answer = (status: number, body: unknown = {}): ProviderResponse => ({ status, headers: {}, body: Buffer.from(JSON.stringify(body)) });

it.each(['sendMessage', 'replyToMessage'] as const)('%s releases a provably unsent dispatch and retries the same draft', async operation => {
  const context = new HeartbeatContext(undefined);
  const scenario = { callId: 'send-call', operation } as Scenario;
  const call = scenarioCall(scenario, connection, 'outlook-mail', context, { idempotencyKey: 'send-call' }) as Call;
  const requests: string[] = [];
  let sendCount = 0;
  const transport = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const path = new URL(request.url).pathname;
    requests.push(`${request.method} ${path}`);
    if (request.method === 'GET' && path.endsWith('/messages')) return answer(200, { value: [] });
    if (request.method === 'POST' && (path.endsWith('/messages') || path.endsWith('/createReply'))) {
      return answer(201, { id: 'draft1', isDraft: true, toRecipients: [{ emailAddress: { address: 'a@example.com' } }] });
    }
    if (request.method === 'PATCH') return answer(200);
    if (request.method === 'GET' && path.endsWith('/draft1')) return answer(200, { id: 'draft1', isDraft: true });
    if (request.method === 'POST' && path.endsWith('/draft1/send')) {
      sendCount++;
      if (sendCount === 1) throw new ProviderTransportError(true);
      return answer(202);
    }
    throw new Error(`unexpected request ${request.method} ${path}`);
  };
  const client = new OutlookMailClient({}, new StaticCredentialProvider([[connection, credentials]]), { refreshDriver: driver, transport });
  const sender = operation === 'sendMessage' ? client.sendMessage() : client.replyToMessage();
  const input = operation === 'sendMessage'
    ? { to: ['a@example.com'], subject: 'Test', text: 'Body' }
    : { messageId: 'original', text: 'Body' };
  const first = await sender.invoke(call, input as never);
  expect(first).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
  expect(context.marker).toMatchObject({ outlookMailSendPhase: 'draftReady', outlookMailDraftMessageId: 'draft1' });
  const second = await sender.invoke(call, input as never);
  expect(second).toMatchObject({ kind: 'branch', branch: 'sent' });
  expect(sendCount).toBe(2);
  expect(requests.filter(request => request.endsWith('/draft1/send'))).toHaveLength(2);
});

it('classifies an unreadable send checkpoint before credential resolution', async () => {
  const context = new HeartbeatContext({ value: { invalid: true }, attempt: 1 });
  const scenario = { callId: 'send-call', operation: 'sendMessage' } as Scenario;
  const call = scenarioCall(scenario, connection, 'outlook-mail', context, { idempotencyKey: 'send-call' }) as Call;
  let resolutions = 0;
  const client = new OutlookMailClient({}, { resolve: async () => { resolutions++; throw new Error('unavailable'); } }, { refreshDriver: driver });
  const attempt = await client.sendMessage().invoke(call, { to: ['a@example.com'], subject: 'Test', text: 'Body' });
  expect(attempt).toMatchObject({ kind: 'uncertain', failure: { kind: 'LOCAL_DEFECT' } });
  expect(resolutions).toBe(0);
});

it('chooses the earliest draft by parsed instant rather than timestamp text', async () => {
  const context = new HeartbeatContext(undefined);
  const call = scenarioCall({ callId: 'draft-order', operation: 'sendMessage' } as Scenario,
    connection, 'outlook-mail', context, { idempotencyKey: 'draft-order' }) as Call;
  let sentPath = '';
  const client = new OutlookMailClient({}, new StaticCredentialProvider([[connection, credentials]]), { refreshDriver: driver,
    transport: async request => {
      const path = new URL(request.url).pathname;
      if (request.method === 'GET' && path.endsWith('/messages')) return answer(200, { value: [
        { id: 'draftLater', isDraft: true, createdDateTime: '2026-01-01T09:00:00Z' },
        { id: 'draftEarlier', isDraft: true, createdDateTime: '2026-01-01T10:00:00+02:00' },
      ] });
      if (request.method === 'POST' && path.endsWith('/send')) { sentPath = path; return answer(202); }
      throw new Error('unexpected request');
    } });
  const result = await client.sendMessage().invoke(call, { to: ['a@example.com'], subject: 'Test', text: 'Body' });
  expect(result).toMatchObject({ kind: 'branch', branch: 'sent' });
  expect(sentPath).toContain('/draftEarlier/send');
});
