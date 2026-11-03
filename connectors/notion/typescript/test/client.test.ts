// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { CredentialBrokerUnavailableError } from '@superdurable/connectors-sdk/projectconfig';
import { HeartbeatContext, scenarioCall } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { NotionClient } from '../src/client.js';

const connection = { provider: 'notion', name: 'notion-test' };
function call(operation: string): Call {
  const scenario = { name: 'unit', operation, callId: '64b6abdd-fb4b-5576-b67c-e695ee42f5fc', input: {}, exchanges: [], expected: { branch: '', failureKind: '', retry: false, retryAfterMs: 0, uncertain: false, requestCount: 0 } };
  return scenarioCall(scenario, connection, 'notion', new HeartbeatContext(undefined), { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
}

it('keeps terminal credential errors distinct from transient broker errors', async () => {
  let requests = 0;
  const sender = async () => { requests++; return { status: 200, headers: {}, body: Buffer.from('{}') }; };
  const terminal = new NotionClient({}, { resolve: async () => { throw new ReauthorizationRequiredError(); } }, { send: sender });
  const rejected = await terminal.search().invoke(call('search'), {});
  expect(rejected.kind).toBe('branch');
  if (rejected.kind === 'branch') { expect(rejected.branch).toBe('defect'); expect(rejected.failure?.kind).toBe('AUTHENTICATION'); }
  const transient = new NotionClient({}, { resolve: async () => { throw new CredentialBrokerUnavailableError(); } }, { send: sender });
  const retry = await transient.search().invoke(call('search'), {});
  expect(retry.kind).toBe('retry');
  if (retry.kind === 'retry') expect(retry.failure.kind).toBe('AUTHENTICATION');
  expect(requests).toBe(0);
});

it('rejects a display-name email before any update request', async () => {
  let requests = 0;
  const client = new NotionClient({}, { resolve: async () => ({ apiToken: new SecretString('ntn_test_token_value') }) }, { send: async () => { requests++; return { status: 200, headers: {}, body: Buffer.from('{}') }; } });
  const result = await client.updatePageProperties().invoke(call('updatePageProperties'), {
    pageId: '1f3c9a2e-5b7d-4e8a-9c01-23456789abcd', properties: { Email: { type: 'email', email: 'Ada <ada@example.com>' } },
  });
  expect(result.kind).toBe('branch');
  if (result.kind === 'branch') expect(result.branch).toBe('defect');
  expect(requests).toBe(0);
});

it('records a not-sent checkpoint before retrying unavailable create credentials', async () => {
  const context = new HeartbeatContext(undefined);
  const scenario = { name: 'create-credentials', operation: 'createPage', callId: '64b6abdd-fb4b-5576-b67c-e695ee42f5fc', input: {}, exchanges: [], expected: { branch: '', failureKind: '', retry: false, retryAfterMs: 0, uncertain: false, requestCount: 0 } };
  const invocation = scenarioCall(scenario, connection, 'notion', context, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  const client = new NotionClient({}, { resolve: async () => { throw new CredentialBrokerUnavailableError(); } }, { send: async () => { throw new Error('provider must not be called'); } });
  const attempt = await client.createPage().invoke(invocation, { dataSourceId: '2a4d6f80-1b3c-4d5e-8f70-112233445566' });
  expect(attempt.kind).toBe('retry');
  expect(context.marker).toMatchObject({ state: 'notSent', callId: invocation.id });
});
