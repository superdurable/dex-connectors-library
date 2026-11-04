// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { CodaClient, type CodaRequestSender } from '../src/client.js';

const connection = { provider: 'coda', name: 'coda' };
const call: Call = {
  context: { flowId: 'coda-unit', cancellationSignal: new AbortController().signal } as Call['context'],
  id: 'unit-call', connection, operation: { connectorId: 'coda', operationId: 'listRows' },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};

describe('Coda operation boundaries', () => {
  it('rejects invalid IDs and limits before resolving a credential or sending a request', async () => {
    let resolved = 0;
    let sent = 0;
    const client = new CodaClient({}, {
      resolve: async () => { resolved++; return { apiToken: new SecretString('inert-token') }; },
    }, { send: async () => { sent++; throw new Error('unexpected request'); } });
    const result = await client.listRows().invoke(call, { docId: 'bad/doc', tableId: 'grid-rows', limit: 501 });
    expect(result.kind).toBe('branch');
    if (result.kind !== 'branch') throw new Error('expected a terminal branch');
    expect(result.branch).toBe('defect');
    expect(result.failure?.kind).toBe('VALIDATION');
    expect(resolved).toBe(0);
    expect(sent).toBe(0);
  });

  it('resolves credentials on each call and never exposes provider error bodies', async () => {
    const tokens: string[] = [];
    let credentialVersion = 0;
    const sender: CodaRequestSender = async request => {
      tokens.push(request.headers?.Authorization ?? '');
      return { status: 401, headers: {}, body: Buffer.from('{"message":"private sentinel"}') };
    };
    const client = new CodaClient({}, {
      resolve: async () => ({ apiToken: new SecretString(`inert-token-${++credentialVersion}`) }),
    }, { send: sender });
    const input = { docId: 'AbCDeFGH', tableId: 'grid-pqRst-U', rowId: 'i-Meridian' };
    const first = await client.getRow().invoke(call, input);
    const second = await client.getRow().invoke(call, input);
    expect(tokens).toEqual(['Bearer inert-token-1', 'Bearer inert-token-2']);
    expect(first.kind).toBe('branch');
    if (first.kind !== 'branch') throw new Error('expected a terminal branch');
    expect(first.branch).toBe('providerRejected');
    expect(second.failure?.message).not.toContain('private sentinel');
  });
});
