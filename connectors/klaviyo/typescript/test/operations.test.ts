// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { KlaviyoClient } from '../src/client.js';

const call = {
  id: 'klaviyo-provider-call', connection: { provider: 'klaviyo', name: 'test' },
  operation: { connectorId: 'klaviyo', operationId: 'getProfile' },
  context: { flowId: 'klaviyo-provider-test', cancellationSignal: new AbortController().signal },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
} as unknown as Call;

it('escapes opaque profile IDs like Go url.PathEscape', async () => {
  let requestUrl = '';
  const client = new KlaviyoClient({}, { resolve: async () => ({ privateApiKey: new SecretString('pk_test_key') }) }, {
    send: async (request: ProviderRequest) => {
      requestUrl = request.url;
      return { status: 200, headers: {}, body: Buffer.from('{"data":{"type":"profile","id":"profile,one+two 😀"}}') };
    },
  });
  const result = await client.getProfile().invoke(call, { id: "profile,one;two!it's(ok)*+$=:@ 😀" });
  expect(result.kind).toBe('branch');
  expect(requestUrl).toBe('https://a.klaviyo.com/api/profiles/profile%2Cone%3Btwo%21it%27s%28ok%29%2A+$=:@%20%F0%9F%98%80');
});

it('treats terminal credential failures as a defect before transmission', async () => {
  for (const error of [new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError()]) {
    const client = new KlaviyoClient({}, { resolve: async () => { throw error; } }, {
      send: async () => { throw new Error('unexpected provider request'); },
    });
    const result = await client.getProfile().invoke(call, { id: 'profile-1' });
    expect(result, error.name).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
    expect(JSON.stringify(result)).not.toContain('sensitive credential detail');
  }
});

it('retries transient credential failures before transmission', async () => {
  for (const error of [new Error('connection is not configured'), new CredentialBrokerUnavailableError(), new RangeError('sensitive credential detail'), new TypeError('sensitive credential detail')]) {
    const client = new KlaviyoClient({}, { resolve: async () => { throw error; } }, {
      send: async () => { throw new Error('unexpected provider request'); },
    });
    const eventCall = { ...call, idempotencyKey: 'klaviyo-provider-call' } as Call;
    const result = await client.createEvent().invoke(eventCall, { email: 'member@meridian.co', metricName: 'Signed Up' });
    expect(result, error.name).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(JSON.stringify(result)).not.toContain('sensitive credential detail');
  }
});

it('retries a refused connection and an oversized 429 response', async () => {
  const credentialSource = { resolve: async () => ({ privateApiKey: new SecretString('pk_test_key') }) };
  const refused = new KlaviyoClient({}, credentialSource, { send: async () => { throw new ProviderTransportError(true); } });
  expect(await refused.getProfile().invoke(call, { id: 'profile-1' })).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
  const limited = new KlaviyoClient({}, credentialSource, { send: async () => { throw new BodyTooLargeError(429, { 'retry-after': '3' }); } });
  expect(await limited.getProfile().invoke(call, { id: 'profile-1' })).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' }, retryAfterMs: 3000 });
});
