// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { MailchimpClient } from '../src/client.js';
import { SendCampaignDefinition, UpsertMemberDefinition } from '../src/generated/definitions.js';
import type { SendCampaignOutput } from '../src/generated/models.js';

const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';
const campaignId = '42694e9e57';
const listId = '57afe96172';

class AttemptContext {
  attempt = 1;
  marker: unknown;
  heartbeats = 0;
  readonly runId = 'run';
  readonly flowId = 'send-campaign-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 60_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.heartbeats++; this.marker = value; }
}

interface Answer { status: number; body: string }

function campaign(status: string): Answer {
  return { status: 200, body: JSON.stringify({ id: campaignId, web_id: 42, type: 'regular', status, emails_sent: 0, create_time: '2026-10-25T10:00:00Z',
    settings: { title: 'Spring Launch', subject_line: 'Hello' }, recipients: { list_id: listId, list_name: 'Audience', recipient_count: 100 } }) };
}
const serverError: Answer = { status: 500, body: '{"title":"Internal Server Error"}' };
const badRequest: Answer = { status: 400, body: '{"title":"Bad Request"}' };

/** Answers reads with the current status and each send with its next scripted reply, then accepts. */
class CampaignScript {
  sends = 0;
  readonly requests: ProviderRequest[] = [];
  constructor(public status: string, private readonly sendReplies: Array<() => Answer> = []) {}
  answer(request: ProviderRequest): Answer {
    this.requests.push(request);
    if (request.method === 'GET') return campaign(this.status);
    this.sends++;
    const reply = this.sendReplies[this.sends - 1];
    if (reply) return reply();
    this.status = 'sending';
    return { status: 204, body: '' };
  }
}

function newClient(answer: (request: ProviderRequest) => Answer): MailchimpClient {
  return new MailchimpClient({}, { resolve: async () => ({ apiKey: new SecretString('mailchimpfixturekey-us6') }) }, {
    send: async (request: ProviderRequest) => {
      const { status, body } = answer(request);
      return { status, headers: {}, body: Buffer.from(body) };
    },
  });
}

async function send(script: CampaignScript, context: AttemptContext, maxSendsOverride?: number): Promise<MutationResult<SendCampaignOutput> | RetryError> {
  const call = { id: callId, connection: { provider: 'mailchimp', name: 'test' }, operation: SendCampaignDefinition.operation, context,
    ...(maxSendsOverride === undefined ? {} : { maxSendsOverride }) } as unknown as Call;
  try { return await runMutation(newClient(request => script.answer(request)).sendCampaign(), call, { campaignId, expectedListId: listId }); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

describe('Mailchimp sendCampaign bounded resend', () => {
  it('resends an unknown outcome after reading a draft below the cap', async () => {
    const script = new CampaignScript('save', [() => serverError]);
    const context = new AttemptContext();
    expect(await send(script, context)).toBeInstanceOf(RetryError);
    expect(context.marker).toMatchObject({ state: 'claimed', sendCount: 1 });
    context.attempt = 2;
    expect(await send(script, context)).toMatchObject({ branch: 'sent' });
    expect(script.sends).toBe(2);
    expect(context.marker).toMatchObject({ state: 'claimed', sendCount: 2 });
  });

  it('reads back an earlier send and never resends it', async () => {
    const script = new CampaignScript('save', [() => serverError]);
    const context = new AttemptContext();
    expect(await send(script, context)).toBeInstanceOf(RetryError);
    script.status = 'sending';
    context.attempt = 2;
    expect(await send(script, context)).toMatchObject({ branch: 'alreadySent' });
    expect(script.sends).toBe(1);
  });

  it('selects uncertain only at the send cap', async () => {
    const script = new CampaignScript('save', [() => serverError, () => serverError, () => serverError]);
    const context = new AttemptContext();
    for (let attempt = 1; attempt < SendCampaignDefinition.maxSends!; attempt++) {
      context.attempt = attempt;
      expect(await send(script, context)).toBeInstanceOf(RetryError);
    }
    context.attempt = SendCampaignDefinition.maxSends!;
    expect(await send(script, context)).toMatchObject({ branch: 'uncertain' });
    context.attempt++;
    expect(await send(script, context)).toMatchObject({ branch: 'uncertain' });
    expect(script.sends).toBe(SendCampaignDefinition.maxSends);
    const single = new CampaignScript('save', [() => serverError]);
    expect(await send(single, new AttemptContext(), 1)).toMatchObject({ branch: 'uncertain' });
    expect(single.sends).toBe(1);
  });

  it('selects a definite refusal of the first send after recording canResend', async () => {
    const script = new CampaignScript('save', [() => badRequest]);
    const context = new AttemptContext();
    expect(await send(script, context)).toMatchObject({ branch: 'providerRejected' });
    expect(context.marker).toMatchObject({ state: 'canResend' });
    expect(script.sends).toBe(1);
  });

  it('reads the campaign after a refused resend instead of selecting the refusal', async () => {
    const script = new CampaignScript('save', [() => serverError, () => badRequest]);
    const context = new AttemptContext();
    expect(await send(script, context)).toBeInstanceOf(RetryError);
    context.attempt = 2;
    expect(await send(script, context)).toMatchObject({ branch: 'uncertain' });
    expect(script.requests).toHaveLength(5);
  });

  it('retries a 429 or 503 send without counting it', async () => {
    for (const reply of [{ status: 429, body: '{"title":"Too Many Requests"}' }, { status: 503, body: '{"title":"Service Unavailable"}' }]) {
      const script = new CampaignScript('save', [() => reply]);
      const context = new AttemptContext();
      expect(await send(script, context)).toBeInstanceOf(RetryError);
      expect(context.marker).toMatchObject({ state: 'canResend' });
      expect((context.marker as { sendCount?: number }).sendCount).toBeUndefined();
      context.attempt = 2;
      expect(await send(script, context)).toMatchObject({ branch: 'sent' });
      expect(context.marker).toMatchObject({ sendCount: 1 });
    }
  });

  it('retries 409, 425, and redirects after a send', async () => {
    for (const status of [409, 425, 307]) {
      const script = new CampaignScript('save', [() => ({ status, body: '{"title":"Unknown Outcome"}' })]);
      expect(await send(script, new AttemptContext())).toBeInstanceOf(RetryError);
    }
  });

  it('retries an unreadable checkpoint without a request', async () => {
    const script = new CampaignScript('save');
    const context = new AttemptContext();
    context.marker = 'not a checkpoint';
    context.attempt = 2;
    expect(await send(script, context)).toBeInstanceOf(RetryError);
    expect(script.requests).toHaveLength(0);
  });

  it('never sends again after a pre-fence checkpoint', async () => {
    for (const [status, branch] of [['sending', 'alreadySent'], ['save', 'uncertain']] as const) {
      const script = new CampaignScript(status);
      const context = new AttemptContext();
      context.marker = { mailchimpDispatchedCallId: callId };
      context.attempt = 2;
      expect(await send(script, context)).toMatchObject({ branch });
      expect(script.sends).toBe(0);
      expect(context.heartbeats).toBe(0);
    }
  });

  it('retries upsertMember after 409 or 425', async () => {
    for (const answer of [{ status: 409, body: '{"title":"Conflict"}' }, { status: 425, body: '{"title":"Too Early"}' }]) {
      const call = { id: callId, connection: { provider: 'mailchimp', name: 'test' }, operation: UpsertMemberDefinition.operation, context: new AttemptContext() } as unknown as Call;
      const attempt = await newClient(() => answer).upsertMember().invoke(call, { listId, emailAddress: 'ben@example.com', statusIfNew: 'subscribed' });
      expect(attempt.kind).toBe('retry');
    }
  });
});
