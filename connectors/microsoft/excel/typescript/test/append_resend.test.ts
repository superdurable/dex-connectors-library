// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError, RejectedCredentialNotRefreshedError } from '@superdurable/connectors-sdk/projectconfig';
import { ExcelClient } from '../src/client.js';
import { AppendTableRowsDefinition, UpdateValuesDefinition } from '../src/generated/definitions.js';
import type { Credentials } from '../src/models.js';
import type { CredentialSource } from '../src/generated/models.js';

const callId = '6072a0ce-9ddb-5937-8998-675e48cfa408';
const connection = { provider: 'microsoft', name: 'approvals' };
const columns = '{"value":[{"id":"1","index":0,"name":"RequestId"}]}';
const emptyKeys = '{"rowCount":1,"columnCount":1,"values":[[""]]}';
const storedKeys = '{"rowCount":2,"columnCount":1,"values":[[""],["REQ-1"]]}';
const credentials: Credentials = { authMethodId: 'microsoft-oauth', accessToken: new SecretString('inert-token'),
  oauthClientId: '', oauthClientSecret: new SecretString(''), refreshToken: new SecretString('') };
const refreshDriver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false, refresh: async () => { throw new Error('unexpected refresh'); } };

class AttemptContext {
  marker: unknown;
  heartbeatCount = 0;
  readonly runId = 'run';
  readonly flowId = 'excel-resend-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  constructor(public attempt = 1) {}
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.heartbeatCount++; this.marker = value; }
}

type Respond = (request: ProviderRequest) => Promise<ProviderResponse>;

function respondJSON(status: number, body: string): ProviderResponse {
  return { status, headers: {}, body: Buffer.from(body) };
}

async function run(respond: Respond, context: AttemptContext, source?: CredentialSource): Promise<string> {
  const client = new ExcelClient({}, source ?? new StaticCredentialProvider([[connection, credentials]]),
    { endpoint: 'http://127.0.0.1:12345/v1.0', transport: respond, refreshDriver });
  const call = { id: callId, connection, operation: AppendTableRowsDefinition.operation, context } as unknown as Call;
  try {
    const result = await runMutation(client.appendTableRows(), call, { driveId: 'b!drive', workbookId: '01WORKBOOK', table: 'Decisions', keyColumn: 'RequestId', rows: [{ RequestId: 'REQ-1' }] });
    return result.branch;
  } catch (error) {
    if (error instanceof RetryError) return 'retry';
    throw error;
  }
}

function failingSource(error: Error): CredentialSource {
  return { resolve: async () => { throw error; } } as unknown as CredentialSource;
}

const noRequest: Respond = async () => { throw new Error('no request is expected'); };

describe('Excel appendTableRows bounded resend', () => {
  it('selects appended when a 2xx body breaks and never resends', async () => {
    const posts: ProviderRequest[] = [];
    const respond: Respond = async request => {
      if (request.method === 'POST') { posts.push(request); throw new ProviderTransportError(false, 201, {}); }
      return respondJSON(200, request.url.includes('/columns?') ? columns : emptyKeys);
    };
    expect(await run(respond, new AttemptContext())).toBe('appended');
    expect(posts).toHaveLength(1);
  });

  it('selects appended when the write window cuts a 2xx body and never resends', async () => {
    const posts: ProviderRequest[] = [];
    const respond: Respond = async request => {
      if (request.method !== 'POST') return respondJSON(200, request.url.includes('/columns?') ? columns : emptyKeys);
      posts.push(request);
      // The 2xx headers arrived; the body stalls until the Fence write window aborts the request.
      await new Promise(resolve => request.signal?.addEventListener('abort', resolve, { once: true }));
      throw new ProviderTransportError(false, 201, {});
    };
    const context = new AttemptContext();
    context.deadline = new Date(Date.now() + 1_200);
    expect(await run(respond, context)).toBe('appended');
    expect(posts).toHaveLength(1);
  });

  it('reads the key column before resending after a throttled resend', async () => {
    const statuses = [502, 429];
    const methods: string[] = [];
    let isFirstAppendVisible = false;
    const respond: Respond = async request => {
      methods.push(request.method);
      if (request.method === 'POST') return respondJSON(statuses.shift()!, '{}');
      return respondJSON(200, request.url.includes('/columns?') ? columns : isFirstAppendVisible ? storedKeys : emptyKeys);
    };
    const context = new AttemptContext();
    expect(await run(respond, context)).toBe('retry');
    context.attempt++;
    expect(await run(respond, context)).toBe('retry');
    context.attempt++;
    isFirstAppendVisible = true;
    expect(await run(respond, context)).toBe('appended');
    expect(methods.filter(method => method === 'POST')).toHaveLength(2);
    expect(methods.slice(-2)).toEqual(['GET', 'GET']);
  });

  it('retries a transient credential failure and never records notSent after a possible send', async () => {
    const outage = failingSource(new Error('credential store offline'));
    const first = new AttemptContext();
    expect(await run(noRequest, first, outage)).toBe('retry');
    expect(first.heartbeatCount).toBe(1);
    const afterPossibleSend = new AttemptContext(2);
    expect(await run(noRequest, afterPossibleSend, outage)).toBe('retry');
    expect(afterPossibleSend.heartbeatCount).toBe(0);
  });

  it('selects uncertain for a terminal credential failure after a possible send', async () => {
    const revoked = failingSource(new ReauthorizationRequiredError());
    expect(await run(noRequest, new AttemptContext(), revoked)).toBe('defect');
    expect(await run(noRequest, new AttemptContext(2), revoked)).toBe('uncertain');
  });

  it('retries a transient credential failure of updateValues', async () => {
    const branches: string[] = [];
    for (const error of [new Error('credential store offline'), new ReauthorizationRequiredError()]) {
      const client = new ExcelClient({}, failingSource(error), { endpoint: 'http://127.0.0.1:12345/v1.0', transport: noRequest, refreshDriver });
      const call = { id: callId, connection, operation: UpdateValuesDefinition.operation, context: new AttemptContext() } as unknown as Call;
      try { branches.push((await runMutation(client.updateValues(), call, { driveId: 'b!drive', workbookId: '01WORKBOOK', worksheet: 'Summary', address: 'A1', values: [['ready']] })).branch); }
      catch (error) { if (error instanceof RetryError) branches.push('retry'); else throw error; }
    }
    expect(branches).toEqual(['retry', 'defect']);
  });

  const terminalCredentialErrors: Error[] = [new ReauthorizationRequiredError(), new ObjectNotFoundError(), new ConnectionNotSelectedError(), new CredentialBrokerRejectedError()];
  const transientCredentialErrors: Error[] = [new CredentialBrokerUnavailableError(), new RejectedCredentialNotRefreshedError(), new Error('connection is not configured'), new TypeError('unlisted')];

  it.each([...terminalCredentialErrors, ...transientCredentialErrors].map(error => [error.name, error] as const))('classifies %s like every connector', async (_name, error) => {
    const isTerminal = terminalCredentialErrors.includes(error);
    expect(await run(noRequest, new AttemptContext(), failingSource(error))).toBe(isTerminal ? 'defect' : 'retry');
    expect(await run(noRequest, new AttemptContext(2), failingSource(error))).toBe(isTerminal ? 'uncertain' : 'retry');
  });

  it('retries a static connection that is not configured', async () => {
    const other = new StaticCredentialProvider([[{ provider: 'microsoft', name: 'other' }, credentials]]);
    expect(await run(noRequest, new AttemptContext(), other as unknown as CredentialSource)).toBe('retry');
  });

  it('treats credential material that fails local checks as terminal', async () => {
    const unsafe = new StaticCredentialProvider([[connection, { ...credentials, accessToken: new SecretString('bad\ntoken') }]]);
    expect(await run(noRequest, new AttemptContext(), unsafe as unknown as CredentialSource)).toBe('defect');
    expect(await run(noRequest, new AttemptContext(2), unsafe as unknown as CredentialSource)).toBe('uncertain');
  });

  it.each([['empty', ''], ['oversized', 'x'.repeat(5 << 20)]])('treats a refusal with an %s body as unknown', async (_name, body) => {
    const respond: Respond = async request => {
      if (request.method === 'POST') {
        if (body.length > (request.maxResponseBytes ?? 0)) throw new BodyTooLargeError(403, {});
        return respondJSON(403, body);
      }
      return respondJSON(200, request.url.includes('/columns?') ? columns : emptyKeys);
    };
    expect(await run(respond, new AttemptContext())).toBe('retry');
  });
});
