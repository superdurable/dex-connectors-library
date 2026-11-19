// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { DropboxClient } from '../src/client.js';
import { CredentialMaterialError, decodeCredentials } from '../src/generated/models.js';
import type { Credentials } from '../src/models.js';

const call = {
  id: '6d3b1f0e-7a2c-5b9d-8e4f-1a2b3c4d5e6f', connection: { provider: 'dropbox', name: 'dropbox' },
  operation: { connectorId: 'dropbox', operationId: 'uploadFile' },
  context: { cancellationSignal: new AbortController().signal },
} as Call;

it.each([
  ['broker unavailable', new CredentialBrokerUnavailableError(), 'retry'],
  ['static not configured', new Error('connection is not configured'), 'retry'],
  ['unlisted error', new RangeError('credential store hiccup'), 'retry'],
  ['reauthorization', new ReauthorizationRequiredError(), 'branch'],
  ['object not found', new ObjectNotFoundError(), 'branch'],
  ['connection not selected', new ConnectionNotSelectedError(), 'branch'],
  ['broker rejected', new CredentialBrokerRejectedError(), 'branch'],
  ['malformed stored credential', new CredentialMaterialError('credential access_token is required'), 'branch'],
  ['SDK-wrapped malformed stored credential', sdkDecodingFailure(), 'branch'],
])('classifies a %s credential failure before any upload request', async (_name, error, kind) => {
  let requests = 0;
  const client = new DropboxClient({}, { resolve: async () => { throw error; } }, {
    send: async () => { requests++; throw new Error('no request may be sent'); },
  });
  const result = await client.uploadFile().invoke(call, { path: '/Projects/example.txt', contentBase64: 'SGVsbG8=' });
  expect(result.kind).toBe(kind);
  if (result.kind === 'branch') expect(result.branch).toBe('defect');
  // A later attempt may follow a stored overwrite, so even a terminal failure retries.
  const later = await client.uploadFile().invoke({ ...call, context: { ...call.context, attempt: 2 } } as Call, { path: '/Projects/example.txt', contentBase64: 'SGVsbG8=' });
  expect(later.kind).toBe('retry');
  expect(requests).toBe(0);
});

/** Reproduce the SDK refresh provider, which replaces the decoder error with this TypeError. */
function sdkDecodingFailure(): TypeError {
  try { decodeCredentials({ access_token: 7 }); } catch { return new TypeError('project credential decoding failed'); }
  throw new Error('malformed material decoded');
}

const credentials: Credentials = {
  oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'),
  accessToken: new SecretString('test-token'), refreshToken: new SecretString('refresh-token'),
};
const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false, refresh: async () => { throw new Error('unexpected refresh'); } };

it.each([
  ['a definite refusal', 403, '{"error_summary":"access_denied/..."}', 'branch'],
  ['a documented endpoint error', 409, '{"error_summary":"path/insufficient_space/..","error":{".tag":"path"}}', 'branch'],
  ['a 409 without a summary', 409, 'not json', 'retry'],
  ['an unlisted status', 412, '{"error_summary":"x"}', 'retry'],
  ['a refusal carrying a stored file', 400, '{"id":"id:example","name":"example.txt","path_display":"/Projects/example.txt"}', 'retry'],
  ['a token echoed in a 2xx', 200, '{"echo":"test-token"}', 'retry'],
  ['malformed 2xx metadata', 200, '{"id":"id:example"}', 'retry'],
] as const)('selects providerRejected only for a definite answer: %s', async (_name, status, body, kind) => {
  const client = new DropboxClient({}, { resolve: async () => credentials }, {
    refreshDriver: driver, send: async () => ({ status, headers: {}, body: Buffer.from(body) }),
  });
  const result = await client.uploadFile().invoke(call, { path: '/Projects/example.txt', contentBase64: 'SGVsbG8=' });
  expect(result.kind).toBe(kind);
  if (result.kind === 'branch') expect(result.branch).toBe('providerRejected');
});
