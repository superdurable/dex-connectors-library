// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it, vi } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { GmailHTTPClient } from '../src/http.js';
import { GmailProvider } from '../src/provider.js';
import { GmailSender } from '../src/send.js';
import type { Credentials } from '../src/models.js';

vi.mock('../src/message.js', async importOriginal => ({
  ...await importOriginal<typeof import('../src/message.js')>(),
  buildMIMEMessage: () => { throw new TypeError('MIME header contains a newline'); },
}));

const connection = { provider: 'google', name: 'gmail-inbox' };
const credentials: Credentials = { accessToken: new SecretString('access-token'), primaryEmail: 'owner@example.com' };
const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false, refresh: async () => { throw new Error('unexpected refresh'); } };

it('reports a MIME encoding failure as "message could not be encoded" without a provider request', async () => {
  let requests = 0;
  const http = new GmailHTTPClient({ endpoint: 'http://127.0.0.1:8080' }, new StaticCredentialProvider([[connection, credentials]]), driver, async () => {
    requests++;
    return { status: 200, headers: {}, body: Buffer.from('{}') };
  });
  const checkpoint: { value?: unknown } = {};
  const call = {
    id: '01234567-89ab-5cde-8f01-23456789abcd', idempotencyKey: '01234567-89ab-5cde-8f01-23456789abcd', connection, operation: { connectorId: 'gmail', operationId: 'sendMessage' },
    context: {
      attempt: 1, runId: 'run-1', cancellationSignal: new AbortController().signal, flowId: 'flow-1',
      hasLastHeartbeatValue: () => checkpoint.value !== undefined,
      getLastHeartbeatValue: () => checkpoint.value,
      recordHeartbeat: async (value: unknown) => { checkpoint.value = value; },
    },
  } as unknown as Call;
  const result = await new GmailSender(http, new GmailProvider(http)).sendMessage(call, { to: ['ada@example.com'], subject: 'Progress', textBody: 'Update' }, new Date(Date.now() + 30_000));
  expect(result).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION', message: 'message could not be encoded' } });
  expect(requests).toBe(0);
});
