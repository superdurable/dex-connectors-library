// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { MixpanelClient } from '../src/client.js';

const call = { context: { cancellationSignal: new AbortController().signal } } as Call;

describe('Mixpanel client construction and credentials', () => {
  it('requires a positive project ID', () => {
    expect(() => new MixpanelClient({ projectId: 0 }, { resolve: async () => ({ serviceAccountUsername: new SecretString('user'), serviceAccountSecret: new SecretString('secret') }) })).toThrow('project ID');
  });
  it('treats unavailable credentials as a terminal authentication defect', async () => {
    const client = new MixpanelClient({ projectId: 12345 }, { resolve: async () => { throw new Error('secret unavailable'); } }, { send: async () => { throw new Error('unexpected request'); } });
    const result = await client.exchange(call, 'trackEvent', 'POST', 'https://api.mixpanel.com/import');
    expect(result).toMatchObject({ disposition: 'defect', failure: { kind: 'AUTHENTICATION' } });
    expect(JSON.stringify(result)).not.toContain('secret unavailable');
  });
  it('retries a refused provider connection as a transport failure', async () => {
    const client = new MixpanelClient({ projectId: 12345 }, { resolve: async () => ({ serviceAccountUsername: new SecretString('user'), serviceAccountSecret: new SecretString('secret') }) }, { send: async () => { throw new Error('ECONNREFUSED'); } });
    const result = await client.exchange(call, 'exportEvents', 'GET', 'https://data.mixpanel.com/api/2.0/export');
    expect(result).toMatchObject({ disposition: 'retry', failure: { kind: 'TRANSPORT' } });
  });
});
