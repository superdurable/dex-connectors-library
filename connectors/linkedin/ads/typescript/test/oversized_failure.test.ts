// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { AdsClient } from '../src/client.js';

const connection = { provider: 'linkedin-ads', name: 'linkedin-ads' };
const credentials = {
  oauthClientId: 'synthetic-client-id', oauthClientSecret: new SecretString('synthetic-client-secret'),
  accessToken: new SecretString('synthetic-linkedin-ads-token'), refreshToken: new SecretString('synthetic-refresh-token'),
};
const call: Call = {
  context: { flowId: 'linkedin-ads-oversized', cancellationSignal: new AbortController().signal } as Call['context'],
  id: 'oversized-call', connection, operation: { connectorId: 'linkedin-ads', operationId: 'listAdAccounts' },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};

function clientAnsweringOversized(status: number): AdsClient {
  return new AdsClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider([[connection, credentials]]), {
    send: () => Promise.reject(new BodyTooLargeError(status, {})),
  });
}

it.each([[429, 'RATE_LIMIT'], [503, 'AVAILABILITY']])('retries an oversized HTTP %i body', async (status, kind) => {
  const attempt = await clientAnsweringOversized(status).listAdAccounts().invoke(call, { pageSize: 2 });
  expect(attempt.kind).toBe('retry');
  expect(attempt.failure?.kind).toBe(kind);
});

it('selects invalidResponse for an oversized HTTP 200 body', async () => {
  const attempt = await clientAnsweringOversized(200).listAdAccounts().invoke(call, { pageSize: 2 });
  expect(attempt.kind).toBe('branch');
  expect(attempt.kind === 'branch' ? attempt.branch : '').toBe('invalidResponse');
});
