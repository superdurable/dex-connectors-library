// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { DropboxClient } from '../src/client.js';

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
])('classifies a %s credential failure before any upload request', async (_name, error, kind) => {
  let requests = 0;
  const client = new DropboxClient({}, { resolve: async () => { throw error; } }, {
    send: async () => { requests++; throw new Error('no request may be sent'); },
  });
  const result = await client.uploadFile().invoke(call, { path: '/Projects/example.txt', contentBase64: 'SGVsbG8=' });
  expect(result.kind).toBe(kind);
  if (result.kind === 'branch') expect(result.branch).toBe('defect');
  expect(requests).toBe(0);
});
