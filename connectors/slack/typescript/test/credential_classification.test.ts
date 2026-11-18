// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, runQuery, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError, RejectedCredentialNotRefreshedError } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { SlackClient } from '../src/client.js';
import { ListChannelsDefinition, PostChannelMessageDefinition } from '../src/generated/definitions.js';

const callId = '74390522-858d-5ed6-bdf7-04e979ba9e4f';

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'credential-classification-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
}

const cases: Array<{ name: string; error: () => Error; terminal: boolean }> = [
  { name: 'reauthorization', error: () => new ReauthorizationRequiredError(), terminal: true },
  { name: 'object not found', error: () => new ObjectNotFoundError(), terminal: true },
  { name: 'connection not selected', error: () => new ConnectionNotSelectedError(), terminal: true },
  { name: 'broker rejected', error: () => new CredentialBrokerRejectedError(), terminal: true },
  { name: 'broker unavailable', error: () => new CredentialBrokerUnavailableError(), terminal: false },
  { name: 'rejected credential not refreshed', error: () => new RejectedCredentialNotRefreshedError(), terminal: false },
  { name: 'static not configured', error: () => new Error('connection is not configured'), terminal: false },
  { name: 'network TypeError', error: () => new TypeError('fetch failed'), terminal: false },
];

/** Build a client whose first validResolutions resolve, after which resolve throws. */
function newClient(error: Error, validResolutions: number) {
  const requests: ProviderRequest[] = [];
  let resolutions = 0;
  const resolve = async () => {
    if (resolutions++ >= validResolutions) throw error;
    return { botToken: new SecretString('bot-token'), userToken: new SecretString('user-token') };
  };
  const client = new SlackClient({}, { resolve, resolveWithRefresh: resolve }, {
    send: async (request: ProviderRequest) => {
      requests.push(request);
      return { status: 500, headers: {}, body: Buffer.from('{"ok":false,"error":"fatal_error"}') };
    },
  });
  return { client, requests };
}

async function post(client: SlackClient, context: AttemptContext) {
  const call = { id: callId, connection: { provider: 'slack', name: 'workspace' }, operation: PostChannelMessageDefinition.operation, context } as unknown as Call;
  try { return await runMutation(client.postChannelMessage(), call, { channelId: 'C123', text: 'hello' }); }
  catch (error) { if (error instanceof RetryError) return error; throw error; }
}

describe('Slack credential classification', () => {
  it.each(cases)('$name before any send', async ({ error, terminal }) => {
    const { client, requests } = newClient(error(), 0);
    const result = await post(client, new AttemptContext());
    expect(requests).toHaveLength(0);
    if (terminal) expect(result).toMatchObject({ branch: 'defect' });
    else expect(result).toBeInstanceOf(RetryError);
  });

  it.each(cases)('$name after a possible send', async ({ error, terminal }) => {
    const { client, requests } = newClient(error(), 1);
    const context = new AttemptContext();
    expect(await post(client, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    const result = await post(client, context);
    expect(requests).toHaveLength(1);
    if (terminal) expect(result).toMatchObject({ branch: 'uncertain' });
    else {
      expect(result).toBeInstanceOf(RetryError);
      expect(JSON.stringify(context.marker)).not.toContain('"notSent"');
    }
  });

  it.each(cases)('$name in a Query', async ({ error, terminal }) => {
    const { client } = newClient(error(), 0);
    const call = { id: callId, connection: { provider: 'slack', name: 'workspace' }, operation: ListChannelsDefinition.operation, context: new AttemptContext() } as unknown as Call;
    const outcome = await runQuery(client.listChannels(), call, {}).catch((caught: unknown) => caught);
    if (terminal) expect(outcome).toMatchObject({ branch: 'defect' });
    else expect(outcome).toBeInstanceOf(RetryError);
  });
});
