// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { PinterestClient } from '../src/client.js';

const connection = { provider: 'pinterest', name: 'pinterest' };

function listPins(status: number) {
  const credentials = {
    oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'),
    accessToken: new SecretString('access-token'), refreshToken: new SecretString('refresh-token'),
  };
  const client = new PinterestClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: 16 }, new StaticCredentialProvider([[connection, credentials]]), {
    send: () => Promise.reject(new BodyTooLargeError(status, {})),
  });
  const call: Call = {
    context: { flowId: 'pinterest-response-limit', cancellationSignal: new AbortController().signal } as Call['context'],
    id: 'pinterest-response-limit-call', connection, operation: { connectorId: 'pinterest', operationId: 'listPins' },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listPins().invoke(call, {});
}

it.each([[429, 'RATE_LIMIT'], [503, 'AVAILABILITY']])('retries an oversized HTTP %i body as %s', async (status, kind) => {
  const attempt = await listPins(status);
  expect(attempt.kind).toBe('retry');
  expect(attempt.failure?.kind).toBe(kind);
});

it('selects invalidResponse for an oversized HTTP 200 body', async () => {
  const attempt = await listPins(200);
  expect(attempt).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'RESPONSE_TOO_LARGE' } });
});
