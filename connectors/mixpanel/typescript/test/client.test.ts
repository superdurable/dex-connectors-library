// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { MixpanelClient } from '../src/client.js';
import { CredentialMaterialError } from '../src/generated/models.js';

const call = { context: { cancellationSignal: new AbortController().signal } } as Call;

describe('Mixpanel client construction and credentials', () => {
  it('requires a positive project ID', () => {
    expect(() => new MixpanelClient({ projectId: 0 }, { resolve: async () => ({ serviceAccountUsername: new SecretString('user'), serviceAccountSecret: new SecretString('secret') }) })).toThrow('project ID');
  });
  it.each([
    ['broker unavailable', new CredentialBrokerUnavailableError()],
    ['static not configured', new Error('connection is not configured')],
    ['unlisted error', new Error('secret unavailable')],
  ])('retries a transient %s credential failure', async (_name, error) => {
    const client = new MixpanelClient({ projectId: 12345 }, { resolve: async () => { throw error; } }, { send: async () => { throw new Error('unexpected request'); } });
    const result = await client.exchange(call, 'trackEvent', 'POST', 'https://api.mixpanel.com/import');
    expect(result).toMatchObject({ disposition: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(JSON.stringify(result)).not.toContain('secret unavailable');
  });
  it.each([
    ['reauthorization', new ReauthorizationRequiredError()],
    ['object not found', new ObjectNotFoundError()],
    ['connection not selected', new ConnectionNotSelectedError()],
    ['broker rejected', new CredentialBrokerRejectedError()],
    ['malformed material', new CredentialMaterialError('credential service_account_secret is required')],
    ['SDK decoding failure', new TypeError('project credential decoding failed')],
  ])('treats a %s credential failure as a terminal authentication defect', async (_name, error) => {
    const client = new MixpanelClient({ projectId: 12345 }, { resolve: async () => { throw error; } }, { send: async () => { throw new Error('unexpected request'); } });
    const result = await client.exchange(call, 'trackEvent', 'POST', 'https://api.mixpanel.com/import');
    expect(result).toMatchObject({ disposition: 'defect', failure: { kind: 'AUTHENTICATION' } });
  });
  it.each([
    ['unlisted status', 425, '{}'],
    ['redirect', 302, ''],
    ['zero imported', 200, '{"code":200,"num_records_imported":0,"status":"OK"}'],
    ['malformed confirmation', 200, 'not json'],
  ])('retries trackEvent after an unknown %s answer', async (_name, status, body) => {
    const client = new MixpanelClient({ projectId: 12345 }, { resolve: async () => ({ serviceAccountUsername: new SecretString('user'), serviceAccountSecret: new SecretString('secret') }) }, { send: async () => ({ status, headers: {}, body: Buffer.from(body) }) });
    const trackCall = { ...call, idempotencyKey: '01234567-89ab-cdef-0123-456789abcdef' } as Call;
    const result = await client.trackEvent().invoke(trackCall, { event: 'Signed Up', distinctID: 'member-1', time: 1793232000 });
    expect(result).toMatchObject({ kind: 'retry' });
  });
  it('rejects trackEvent only after a definite refusal status', async () => {
    const client = new MixpanelClient({ projectId: 12345 }, { resolve: async () => ({ serviceAccountUsername: new SecretString('user'), serviceAccountSecret: new SecretString('secret') }) }, { send: async () => ({ status: 422, headers: {}, body: Buffer.from('{}') }) });
    const trackCall = { ...call, idempotencyKey: '01234567-89ab-cdef-0123-456789abcdef' } as Call;
    const result = await client.trackEvent().invoke(trackCall, { event: 'Signed Up', distinctID: 'member-1', time: 1793232000 });
    expect(result).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
  });
  it('retries a refused provider connection as a transport failure', async () => {
    const client = new MixpanelClient({ projectId: 12345 }, { resolve: async () => ({ serviceAccountUsername: new SecretString('user'), serviceAccountSecret: new SecretString('secret') }) }, { send: async () => { throw new Error('ECONNREFUSED'); } });
    const result = await client.exchange(call, 'exportEvents', 'GET', 'https://data.mixpanel.com/api/2.0/export');
    expect(result).toMatchObject({ disposition: 'retry', failure: { kind: 'TRANSPORT' } });
  });
});
