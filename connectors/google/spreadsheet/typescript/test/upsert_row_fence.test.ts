// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SpreadsheetClient } from '../src/client.js';
import { UpsertRowDefinition } from '../src/generated/definitions.js';
import type { Credentials, UpsertRowOutput } from '../src/generated/models.js';

const callId = '74390522-858d-5ed6-bdf7-04e979ba9e4f';
const header = ['accountId', 'name'];
const ada = ['a-1', 'Ada'];
const input = { spreadsheetId: 'sheet', sheetName: 'Customers', keyColumn: 'accountId', keyValue: 'a-1', values: { accountId: 'a-1', name: 'Ada' } };

class AttemptContext {
  attempt = 1;
  marker: unknown;
  readonly runId = 'run';
  readonly flowId = 'upsert-row-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value; }
  advance(): void { this.attempt++; this.deadline = new Date(Date.now() + 30_000); }
}

type AppendAnswer = (sheet: ScriptedSheet) => ProviderResponse;

/** Serve rows to each GET unless a lookup status is queued; script each POST. */
class ScriptedSheet {
  rows: string[][] = [[...header]];
  posts = 0;
  constructor(readonly appends: AppendAnswer[], readonly lookupStatuses: number[] = []) {}
  respond(request: ProviderRequest): ProviderResponse {
    if (request.method === 'GET') {
      const status = this.lookupStatuses.shift() ?? 0;
      if (status) return { status, headers: {}, body: Buffer.from('') };
      return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ range: 'Customers', values: this.rows })) };
    }
    expect(request.method).toBe('POST');
    this.posts++;
    const answer = this.appends[this.posts - 1];
    if (!answer) throw new Error('unexpected append');
    return answer(this);
  }
}

function appendStatus(status: number, ...addedRows: string[][]): AppendAnswer {
  return sheet => { sheet.rows.push(...addedRows); return { status, headers: {}, body: Buffer.from('') }; };
}

function newClient(sheet: ScriptedSheet, failedResolution = 0, failure: Error = new Error('credential refresh unavailable')): SpreadsheetClient {
  let resolutions = 0;
  const credential = { accessToken: new SecretString('sheets-token'), oauthClientId: '', oauthClientSecret: new SecretString(''), refreshToken: new SecretString('') };
  const resolve = async () => {
    resolutions++;
    if (resolutions === failedResolution) throw failure;
    return credential;
  };
  return new SpreadsheetClient({ endpoint: 'http://localhost' }, { resolve, resolveWithRefresh: resolve }, { send: async request => sheet.respond(request) });
}

async function upsert(client: SpreadsheetClient, context: AttemptContext): Promise<MutationResult<UpsertRowOutput> | RetryError> {
  const call = { id: callId, connection: { provider: 'google-sheets', name: 'workspace' }, operation: UpsertRowDefinition.operation, context } as unknown as Call;
  try { return await runMutation(client.upsertRow(), call, input); }
  catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

async function runAttempts(client: SpreadsheetClient, attempts: number): Promise<MutationResult<UpsertRowOutput> | RetryError> {
  const context = new AttemptContext();
  for (let attempt = 1; attempt < attempts; attempt++) {
    expect(await upsert(client, context), `attempt ${attempt}`).toBeInstanceOf(RetryError);
    context.advance();
  }
  return upsert(client, context);
}

describe('Google Sheets upsertRow bounded append resend', () => {
  it('selects uncertain for duplicate key rows in the sending attempt read-back', async () => {
    const sheet = new ScriptedSheet([appendStatus(500, ada, ada)]);
    expect(await runAttempts(newClient(sheet), 1)).toMatchObject({ branch: 'uncertain' });
    expect(sheet.posts).toBe(1);
  });

  it('selects uncertain for duplicate key rows in a later lookup', async () => {
    const sheet = new ScriptedSheet([appendStatus(500, ada, ada)], [0, 503]);
    expect(await runAttempts(newClient(sheet), 2)).toMatchObject({ branch: 'uncertain' });
    expect(sheet.posts).toBe(1);
  });

  it('retries a failed lookup after a possible append until the send cap', async () => {
    // Lookup, read-back, rejected lookup, lookup, unavailable read-back, rejected lookup at the cap.
    const sheet = new ScriptedSheet([appendStatus(500), appendStatus(500)], [0, 0, 403, 0, 503, 403]);
    expect(await runAttempts(newClient(sheet), 4)).toMatchObject({ branch: 'uncertain' });
    expect(sheet.posts).toBe(2);
  });

  it('reads back after a rate-limited resend', async () => {
    const sheet = new ScriptedSheet([appendStatus(500), appendStatus(429, ada)]);
    expect(await runAttempts(newClient(sheet), 3)).toMatchObject({ branch: 'upserted', value: { action: 'inserted' } });
    expect(sheet.posts).toBe(2);
  });

  it('keeps the send count across a pre-send retry after a rate-limited resend', async () => {
    const sheet = new ScriptedSheet([appendStatus(500), appendStatus(429, ada)]);
    expect(await runAttempts(newClient(sheet, 3), 4)).toMatchObject({ branch: 'upserted', value: { action: 'inserted' } });
    expect(sheet.posts).toBe(2);
  });

  it.each([
    { name: 'unparsable success', answer: (() => ({ status: 200, headers: {}, body: Buffer.from('{"updates":') })) as AppendAnswer, lookups: [] as number[], attempts: 1 },
    { name: 'refusal naming the appended range', answer: (() => ({ status: 400, headers: {}, body: Buffer.from('{"updates":{"updatedRange":"\'Customers\'!A2:B2"}}') })) as AppendAnswer, lookups: [], attempts: 1 },
    { name: 'unavailable read-back then a later lookup', answer: (() => ({ status: 200, headers: {}, body: Buffer.from('{"updates":{}}') })) as AppendAnswer, lookups: [0, 503], attempts: 2 },
    { name: 'success body that breaks', answer: (() => { throw new ProviderTransportError(false, 200, {}); }) as AppendAnswer, lookups: [], attempts: 1 },
  ])('never resends an accepted append whose row is missing: $name', async ({ answer, lookups, attempts }) => {
    const sheet = new ScriptedSheet([answer], [...lookups]);
    expect(await runAttempts(newClient(sheet), attempts)).toMatchObject({ branch: 'uncertain' });
    expect(sheet.posts).toBe(1);
  });

  it.each([
    { name: 'oversized', answer: (() => { throw new BodyTooLargeError(400, {}); }) as AppendAnswer },
    { name: 'broken', answer: (() => { throw new ProviderTransportError(false, 400, {}); }) as AppendAnswer },
  ])('treats a refusal with an $name body as an unknown outcome', async ({ answer }) => {
    const sheet = new ScriptedSheet([answer]);
    expect(await runAttempts(newClient(sheet), 1)).toBeInstanceOf(RetryError);
    expect(sheet.posts).toBe(1);
  });

  it('reads back a success body that breaks and never resends it', async () => {
    const sheet = new ScriptedSheet([current => { current.rows.push(ada); throw new ProviderTransportError(false, 200, {}); }]);
    expect(await runAttempts(newClient(sheet), 1)).toMatchObject({ branch: 'upserted', value: { action: 'inserted' } });
    expect(sheet.posts).toBe(1);
  });

  const credentialFailures = [
    { name: 'reauthorization', failure: new ReauthorizationRequiredError(), isTerminal: true },
    { name: 'object not found', failure: new ObjectNotFoundError(), isTerminal: true },
    { name: 'connection not selected', failure: new ConnectionNotSelectedError(), isTerminal: true },
    { name: 'broker rejected', failure: new CredentialBrokerRejectedError(), isTerminal: true },
    // The SDK project provider replaces a malformed-material decoder error with this TypeError.
    { name: 'malformed stored material', failure: new TypeError('project credential decoding failed'), isTerminal: true },
    { name: 'broker unavailable', failure: new CredentialBrokerUnavailableError(), isTerminal: false },
    { name: 'static connection not configured', failure: new Error('connection is not configured'), isTerminal: false },
  ];

  it.each(credentialFailures)('classifies a $name credential failure on a first send', async ({ failure, isTerminal }) => {
    const sheet = new ScriptedSheet([]);
    const outcome = await upsert(newClient(sheet, 1, failure), new AttemptContext());
    if (isTerminal) expect(outcome).toMatchObject({ branch: 'defect' });
    else expect(outcome).toBeInstanceOf(RetryError);
    expect(sheet.posts).toBe(0);
  });

  it.each(credentialFailures)('classifies a $name credential failure after a possible append', async ({ failure, isTerminal }) => {
    const sheet = new ScriptedSheet([appendStatus(500)]);
    const client = newClient(sheet, 2, failure);
    const context = new AttemptContext();
    expect(await upsert(client, context)).toBeInstanceOf(RetryError);
    context.advance();
    const outcome = await upsert(client, context);
    if (isTerminal) expect(outcome).toMatchObject({ branch: 'uncertain' });
    else expect(outcome).toBeInstanceOf(RetryError);
    expect(sheet.posts).toBe(1);
  });

  it('retries getValues when a static provider has no connection', async () => {
    const client = new SpreadsheetClient({ endpoint: 'http://localhost' }, new StaticCredentialProvider<Credentials>([]), { send: async () => { throw new Error('no request is expected'); } });
    const call = { id: callId, connection: { provider: 'google-sheets', name: 'workspace' }, operation: { connectorId: 'google-spreadsheet', operationId: 'getValues' }, context: new AttemptContext() } as unknown as Call;
    expect(await client.getValues().invoke(call, { spreadsheetId: 'sheet', range: 'Customers!A:B' })).toMatchObject({ kind: 'retry' });
  });
});
