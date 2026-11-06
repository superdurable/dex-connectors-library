// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { TeamsClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const connection = { provider: 'microsoft', name: 'teams-test' };
const credentials: Credentials = { accessToken: new SecretString('token'),
  oauthClientId: 'client', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') };
const call = scenarioCall({ callId: 'teams-status', operation: 'postChatMessage' } as Scenario,
  connection, 'microsoft-teams', new HeartbeatContext(undefined)) as Call;

it.each([429, 404])('keeps HTTP %s when its error body cannot be read', async status => {
  const client = new TeamsClient({}, new StaticCredentialProvider([[connection, credentials]]), {
    transport: { send: async request => { request.onResponseChunk?.(Buffer.from('{'), status, {}); throw new ProviderTransportError(false); } },
  });
  const result = await client.exchange(call, credentials, 'GET', '/teams');
  expect(result.exchange).toMatchObject({ status, dispatched: true });
  expect(result.exchange.error).toBeUndefined();
  expect(result.exchange.bodyReadFailed).toBeUndefined();
});

it('accepts a same-thread link whose path is percent encoded as Go url.Parse decodes it', async () => {
  const teamId = 'fbe2bf47-16c8-47cf-b4a5-4b9b187c508b';
  const channelId = '19:channel@thread.tacv2';
  const messageId = '1616989510408';
  let requests = 0;
  const client = new TeamsClient({}, new StaticCredentialProvider([[connection, credentials]]), {
    transport: { send: async () => { requests++; return { status: 200, headers: {}, body: Buffer.from('{"value":[]}') }; } },
  });
  const cursor = `https://graph.microsoft.com/v1.0/teams/${teamId}/channels/${channelId}/messages/${messageId}/repli%65s`;
  const result = await client.listThreadReplies().invoke(call, { teamId, channelId, messageId, cursor });
  expect(result).toMatchObject({ kind: 'branch', branch: 'read' });
  expect(requests).toBe(1);
});
