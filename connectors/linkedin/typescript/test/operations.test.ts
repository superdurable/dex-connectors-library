// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { LinkedInClient } from '../src/client.js';

const connection = { provider: 'linkedin', name: 'signup' };
const scenario = { name: 'unit', operation: 'getAuthenticatedProfile', callId: '50000000-0000-4000-8000-000000000099', input: {}, exchanges: [], expected: { branch: '', failureKind: '', requestCount: 0, retry: false, retryAfterMs: 0, uncertain: false } } as Scenario;
const call = scenarioCall(scenario, connection, 'linkedin', {
  flowId: 'linkedin-unit', cancellationSignal: new AbortController().signal,
}, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString('one-use-token') }]]);

it('rejects an unsafe UserInfo URL', () => {
  expect(() => new LinkedInClient({ userInfoUrl: 'http://linkedin.example/v2/userinfo' }, credentials)).toThrow();
  expect(() => new LinkedInClient({ userInfoUrl: 'https://token@api.linkedin.com/v2/userinfo' }, credentials)).toThrow();
  expect(() => new LinkedInClient({ userInfoUrl: 'https://api.linkedin.com/v2/userinfo?token=value' }, credentials)).toThrow();
});

it.each([
  ['wrong-typed subject', { sub: 7, email: 'a@example.com', email_verified: true }, 'invalidResponse'],
  ['wrong-typed verification', { sub: 'member', email: 'a@example.com', email_verified: 'true' }, 'invalidResponse'],
  ['unverified email', { sub: 'member', email: 'a@example.com', email_verified: false }, 'verifiedEmailRequired'],
  ['invalid email', { sub: 'member', email: 'not-an-email', email_verified: true }, 'verifiedEmailRequired'],
] as const)('%s follows Go terminal classification', async (_name, body, expectedBranch) => {
  const client = new LinkedInClient({ userInfoUrl: 'http://127.0.0.1/v2/userinfo' }, credentials, {
    send: async () => ({ status: 200, headers: {}, body: new TextEncoder().encode(JSON.stringify(body)) }),
  });
  const result = await client.getAuthenticatedProfile().invoke(call, {});
  expect(result.kind).toBe('branch');
  if (result.kind !== 'branch') throw new Error('expected terminal branch');
  expect(result.branch).toBe(expectedBranch);
});

it('preserves the configured request timeout and response bound', async () => {
  let timeout = 0;
  let bound = 0;
  const client = new LinkedInClient({ userInfoUrl: 'http://127.0.0.1/v2/userinfo', timeoutMs: 2500, maxResponseBytes: 1024 }, credentials, {
    send: async request => {
      timeout = request.timeoutMs ?? 0;
      bound = request.maxResponseBytes ?? 0;
      return { status: 200, headers: {}, body: new TextEncoder().encode('{"sub":"member","email":"a@example.com","email_verified":true}') };
    },
  });
  const result = await client.getAuthenticatedProfile().invoke(call, {});
  expect(result.kind).toBe('branch');
  if (result.kind !== 'branch') throw new Error('expected successful branch');
  expect(result.branch).toBe('profileLoaded');
  expect(timeout).toBe(2500);
  expect(bound).toBe(1024);
});

it.each(['üñîçøðé@example.com', 'user@例子.测试', 'user@[127.0.0.1]', '"a b"@example.com'])('accepts an address accepted by Go mail.ParseAddress: %s', async email => {
  const client = new LinkedInClient({ userInfoUrl: 'http://127.0.0.1/v2/userinfo' }, credentials, {
    send: async () => ({ status: 200, headers: {}, body: new TextEncoder().encode(JSON.stringify({ sub: 'member', email, email_verified: true, locale: { language: 7 } })) }),
  });
  const result = await client.getAuthenticatedProfile().invoke(call, {});
  if (result.kind !== 'branch') throw new Error('expected successful branch');
  expect(result.branch).toBe('profileLoaded');
  expect(result.value.locale).toBeUndefined();
});
