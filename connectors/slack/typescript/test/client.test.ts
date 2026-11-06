// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString } from '@superdurable/connectors-sdk';
import { SlackClient } from '../src/client.js';

const credentials = {
  resolve: async () => ({ botToken: new SecretString('bot-token'), userToken: new SecretString('user-token') }),
  resolveWithRefresh: async () => ({ botToken: new SecretString('bot-token'), userToken: new SecretString('user-token') }),
};

describe('Slack provider response decoding', () => {
  it('accepts JSON null as the Go zero value for optional fields', async () => {
    const client = new SlackClient({}, credentials, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"ok":true,"messages":null,"response_metadata":null}') }) });
    const response = await client.request('GET', 'conversations.replies', 'user-token');
    expect(response.error).toBeUndefined();
  });

  it('rejects wrong-typed Slack fields before an operation selects a branch', async () => {
    const client = new SlackClient({}, credentials, { send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"ok":true,"messages":[{"ts":123}]}') }) });
    const response = await client.request('GET', 'conversations.replies', 'user-token');
    expect(response.error).toBe('invalid');
  });

  it('rejects unsafe HTTP endpoints and invalid response limits during construction', () => {
    expect(() => new SlackClient({ endpoint: 'http://example.com/api' }, credentials)).toThrow('HTTPS');
    expect(() => new SlackClient({ maxResponseBytes: -1 }, credentials)).toThrow('positive integers');
  });
});
