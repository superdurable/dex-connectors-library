// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { AirtableClient } from '../src/client.js';

const input = { baseId: 'appRefundBase0001', tableIdOrName: 'tblPolicies000001' };
const call = {
  id: 'deb1de08-84b3-554e-91d5-e46e685d1f56',
  connection: { provider: 'airtable', name: 'airtable-policies' },
  operation: { connectorId: 'airtable', operationId: 'listRecords' },
  context: { cancellationSignal: new AbortController().signal },
} as Call;

it('routes revoked connection credentials to providerRejected before HTTP', async () => {
  let requestCount = 0;
  const client = new AirtableClient({}, { resolve: async () => { throw new ReauthorizationRequiredError(); } }, {
    send: async () => { requestCount++; throw new Error('unexpected request'); },
  });
  const attempt = await client.listRecords().invoke(call, input);
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') {
    expect(attempt.branch).toBe('providerRejected');
    expect(attempt.failure?.kind).toBe('AUTHENTICATION');
  }
  expect(requestCount).toBe(0);
});

it('escapes text filters in the provider request without sending the token in the body', async () => {
  let requestBody: Record<string, unknown> | undefined;
  const client = new AirtableClient({}, { resolve: async () => ({ personalAccessToken: new SecretString('patTESTtoken.0123456789abcdef') }) }, {
    send: async request => {
      requestBody = JSON.parse(Buffer.from(request.body ?? []).toString('utf8')) as Record<string, unknown>;
      return { status: 200, headers: {}, body: Buffer.from('{"records":[]}', 'utf8') };
    },
  });
  const attempt = await client.listRecords().invoke(call, { ...input, filters: [{ field: 'Policy Key', text: 'refund "vip"' }] });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind === 'branch') expect(attempt.branch).toBe('listed');
  expect(requestBody?.filterByFormula).toBe('{Policy Key} = "refund \\"vip\\""');
  expect(JSON.stringify(requestBody)).not.toContain('patTESTtoken');
});
