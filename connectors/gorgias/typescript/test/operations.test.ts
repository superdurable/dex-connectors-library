// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GorgiasClient, isBareEmail } from '../src/client.js';
import { decodeTicket } from '../src/wire.js';

const credential = { email: 'agent@example.com', apiKey: new SecretString('testKey0123456789') };
const call = {
  id: 'call-test', connection: { provider: 'gorgias', name: 'gorgias-test' }, operation: { connectorId: 'gorgias', operationId: 'getTicket' },
  context: { attempt: 1, cancellationSignal: new AbortController().signal, hasLastHeartbeatValue: () => false },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
} as unknown as Call;

describe('Gorgias client', () => {
  it('rejects malformed domains and limits', () => {
    expect(() => new GorgiasClient({ domain: 'acme.gorgias.com' }, { resolve: async () => credential })).toThrow();
    expect(() => new GorgiasClient({ domain: 'acme', maxResponseBytes: -1 }, { resolve: async () => credential })).toThrow();
  });

  it('classifies a refused connection as safe to retry', async () => {
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, {
      send: async () => { throw new ProviderTransportError(true); },
    });
    const attempt = await client.getTicket().invoke(call, { ticketId: 7001 });
    expect(attempt.kind).toBe('retry');
    expect(attempt.failure?.kind).toBe('TRANSPORT');
  });

  it('treats unavailable credentials as a terminal defect', async () => {
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => { throw new Error('secret-bearing error'); } });
    const attempt = await client.getTicket().invoke(call, { ticketId: 7001 });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new Error('expected a terminal branch');
    expect(attempt.branch).toBe('defect');
    expect(attempt.failure?.kind).toBe('AUTHENTICATION');
    expect(attempt.failure?.message).not.toContain('secret');
  });
});

it.each(['agent@example.com', 'first.last+triage@example.com'])('accepts a bare email: %s', email => expect(isBareEmail(email)).toBe(true));
it.each(['.first@example.com', 'first.@example.com', 'first..last@example.com', 'Name <first@example.com>', 'first@example-.com'])('rejects a malformed email: %s', email => expect(isBareEmail(email)).toBe(false));

it('preserves Go timestamp precision and normalizes offsets', () => {
  const ticket = decodeTicket({ id: 7001, status: 'open', created_datetime: '2026-01-26T14:02:00.384938', updated_datetime: '2026-01-28T09:45:00.932637+01:00' });
  expect(ticket.createdAt).toBe('2026-01-26T14:02:00.384938Z');
  expect(ticket.updatedAt).toBe('2026-01-28T08:45:00.932637Z');
});
