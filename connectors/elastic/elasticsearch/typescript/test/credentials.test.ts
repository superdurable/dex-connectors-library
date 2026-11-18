// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { ElasticsearchClient } from '../src/client.js';

const call = { id: 'elasticsearch-credential-call', context: { cancellationSignal: new AbortController().signal } } as Call;

it.each([
  ['reauthorization required', new ReauthorizationRequiredError(), true],
  ['connection object not found', new ObjectNotFoundError(), true],
  ['connection not selected', new ConnectionNotSelectedError(), true],
  ['credential broker rejected', new CredentialBrokerRejectedError(), true],
  ['credential broker unavailable', new CredentialBrokerUnavailableError(), false],
  ['static connection not configured', new Error('connection is not configured'), false],
  ['unlisted error', new TypeError('unlisted'), false],
])('classifies a %s credential failure without sending', async (_name, credentialFailure, isTerminal) => {
  let requests = 0;
  const client = new ElasticsearchClient({ clusterUrl: 'http://127.0.0.1:12345' }, { resolve: async () => { throw credentialFailure; } }, {
    send: async () => { requests++; throw new Error('unexpected request'); },
  });
  const expected = isTerminal ? { kind: 'branch', branch: 'defect' } : { kind: 'retry', failure: { kind: 'AVAILABILITY' } };
  expect(await client.indexDocument().invoke(call, { index: 'orders', documentId: 'order-1', source: { total: 1 } })).toMatchObject(expected);
  expect(await client.deleteDocument().invoke(call, { index: 'orders', documentId: 'order-1' })).toMatchObject(expected);
  expect(await client.searchDocuments().invoke(call, { index: 'orders', query: 'total:1' })).toMatchObject(expected);
  expect(requests).toBe(0);
});
