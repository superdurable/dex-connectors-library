// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { CloudStorageClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';

const call = {
  id: '6f1d6c2e-3c1f-5b8e-9a43-2a7d5d1f4c10', connection: { provider: 'google-cloud-storage', name: 'google-cloud-storage' },
  operation: { connectorId: 'google-cloud-storage', operationId: 'uploadObject' },
  context: { cancellationSignal: new AbortController().signal },
} as unknown as Call;

it.each([
  { name: 'reauthorization', failure: new ReauthorizationRequiredError(), expected: 'credential' },
  { name: 'object not found', failure: new ObjectNotFoundError(), expected: 'credential' },
  { name: 'connection not selected', failure: new ConnectionNotSelectedError(), expected: 'credential' },
  { name: 'broker rejected', failure: new CredentialBrokerRejectedError(), expected: 'credential' },
  { name: 'broker unavailable', failure: new CredentialBrokerUnavailableError(), expected: 'refresh' },
  { name: 'unlisted', failure: new Error('unlisted'), expected: 'refresh' },
])('classifies a $name resolve failure', async ({ failure, expected }) => {
  const client = new CloudStorageClient({}, { resolve: async () => { throw failure; } }, { send: async () => { throw new Error('no request is expected'); } });
  expect(await client.resolveToken(call)).toEqual({ token: '', error: expected });
});

it('treats an unconfigured static connection as transient', async () => {
  const client = new CloudStorageClient({}, new StaticCredentialProvider<Credentials>([]), { send: async () => { throw new Error('no request is expected'); } });
  expect(await client.resolveToken(call)).toEqual({ token: '', error: 'refresh' });
});
