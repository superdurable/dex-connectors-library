// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { SlackClient } from '../src/client.js';
import { ButtonStyleDanger, ButtonStylePrimary, validatePostButtonMessageInput, type PostButtonMessageInput } from '../src/index.js';

const approvalButtons = () => [
  { actionId: 'approve', text: 'Approve', value: 'order-1042', style: ButtonStylePrimary },
  { actionId: 'reject', text: 'Reject', value: 'order-1042', style: ButtonStyleDanger },
];

function newClient(maxMessageCharacters?: number) {
  const requests: ProviderRequest[] = [];
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => { requests.push(request); return { status: 500, headers: {}, body: Buffer.from('{}') }; };
  const credentials = { botToken: new SecretString('bot-token'), userToken: new SecretString('user-token') };
  const client = new SlackClient({ endpoint: 'http://127.0.0.1:9/api', ...(maxMessageCharacters ? { maxMessageCharacters } : {}) }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }, { send });
  return { client, requests };
}

// The checks run before the fence or a credential read, so a bare call reaches only the validation.
const call = { id: 'call-1', idempotencyKey: 'call-1', connection: { provider: 'slack', name: 'workspace' }, context: { attempt: 1, cancellationSignal: new AbortController().signal } } as unknown as Call;

describe('Slack postButtonMessage', () => {
  const valid = (change: (input: PostButtonMessageInput) => void): PostButtonMessageInput => {
    const input: PostButtonMessageInput = { channelId: 'C123', text: 'Approve?', buttons: approvalButtons() };
    change(input);
    return input;
  };
  const tooManyButtons = Array.from({ length: 26 }, (_, index) => ({ actionId: `action-${'x'.repeat(index + 1)}`, text: 'Choice' }));
  const cases: Record<string, PostButtonMessageInput> = {
    'missing channel': valid(input => { input.channelId = ' '; }),
    'missing text': valid(input => { input.text = ''; }),
    'section text too long': valid(input => { input.text = 'a'.repeat(3001); }),
    'no buttons': valid(input => { input.buttons = null; }),
    'too many buttons': valid(input => { input.buttons = tooManyButtons; }),
    'missing action ID': valid(input => { input.buttons![0]!.actionId = ''; }),
    'long action ID': valid(input => { input.buttons![0]!.actionId = 'a'.repeat(256); }),
    'duplicate action ID': valid(input => { input.buttons![1]!.actionId = 'approve'; }),
    'missing label': valid(input => { input.buttons![0]!.text = ' '; }),
    'long label': valid(input => { input.buttons![0]!.text = 'a'.repeat(76); }),
    'long value': valid(input => { input.buttons![0]!.value = 'a'.repeat(2001); }),
    'unknown style': valid(input => { input.buttons![0]!.style = 'blue'; }),
  };

  it.each(Object.entries(cases))('rejects %s before sending', async (_name, input) => {
    const { client, requests } = newClient();
    expect(validatePostButtonMessageInput(input)).toMatchObject({ provider: 'slack', operation: 'postButtonMessage', kind: 'VALIDATION' });
    expect(await client.postButtonMessage().invoke(call, input)).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
    expect(requests).toEqual([]);
  });

  it('accepts the limits and names a duplicate action ID as the manifest mock does', () => {
    expect(validatePostButtonMessageInput(valid(input => { input.text = 'a'.repeat(3000); input.buttons![0]!.value = 'a'.repeat(2000); }))).toBeUndefined();
    expect(validatePostButtonMessageInput(cases['duplicate action ID']!)?.message).toBe('Each button action ID must be unique.');
  });

  it("applies the connection's maxMessageCharacters", async () => {
    const { client, requests } = newClient(5);
    expect(await client.postButtonMessage().invoke(call, valid(() => undefined))).toMatchObject({ kind: 'branch', branch: 'defect' });
    expect(requests).toEqual([]);
  });
});
