// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { PostmarkClient } from '../src/client.js';

const call: Call = {
  context: { flowId: 'postmark-unit', cancellationSignal: new AbortController().signal } as Call['context'],
  id: '5ecde15b-017b-5681-83c4-dcbd11fe5e99',
  connection: { provider: 'postmark', name: 'postmark' },
  operation: { connectorId: 'postmark', operationId: 'getMessageDetails' },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};

it('escapes a message ID like Go url.PathEscape before sending', async () => {
  let requestedURL = '';
  const client = new PostmarkClient({}, { resolve: async () => ({ apiKey: new SecretString('test-secret-token') }) }, {
    send: async request => {
      requestedURL = request.url;
      return { status: 200, headers: {}, body: Buffer.from('{"MessageID":"msg-1","Subject":"Welcome","Status":"Sent"}') };
    },
  });
  const outcome = await client.getMessageDetails().invoke(call, { messageId: "a/b+$&,'!()" });
  expect(outcome.kind).toBe('branch');
  expect(requestedURL).toBe('https://api.postmarkapp.com/messages/outbound/a%2Fb+$&%2C%27%21%28%29/details');
});

it('keeps missing credentials terminal and a refused connection retryable', async () => {
  const missing = new PostmarkClient({}, { resolve: async () => { throw new Error('secret sentinel'); } });
  const terminal = await missing.getMessageDetails().invoke(call, { messageId: 'msg-1' });
  expect(terminal.kind).toBe('branch');
  if (terminal.kind === 'branch') {
    expect(terminal.branch).toBe('defect');
    expect(terminal.failure?.kind).toBe('AUTHENTICATION');
    expect(terminal.failure?.message).not.toContain('sentinel');
  }
  const refused = new PostmarkClient({}, { resolve: async () => ({ apiKey: new SecretString('test-secret-token') }) }, {
    send: async () => { throw new Error('ECONNREFUSED'); },
  });
  const retry = await refused.getMessageDetails().invoke(call, { messageId: 'msg-1' });
  expect(retry.kind).toBe('retry');
  if (retry.kind === 'retry') expect(retry.failure.kind).toBe('AVAILABILITY');
});
