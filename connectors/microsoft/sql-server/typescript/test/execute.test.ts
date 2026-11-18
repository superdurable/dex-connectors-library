// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { EventEmitter } from 'node:events';
import { afterEach, describe, expect, it, vi } from 'vitest';
import sql from 'mssql';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError, RejectedCredentialNotRefreshedError } from '@superdurable/connectors-sdk/projectconfig';
import { SQLServerClient } from '../src/client.js';
import { ExecuteStatementDefinition, ExecuteStatementNonIdempotentDefinition } from '../src/generated/definitions.js';

/** Keeps the last heartbeat across Execute attempts like Dex does. */
class AttemptContext {
  attempt = 1;
  marker: { state?: string } | undefined;
  readError: Error | undefined;
  readonly runId = 'run';
  readonly flowId = 'sql-server-write-flow';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline = new Date(Date.now() + 30_000);
  readonly cancellationSignal = new AbortController().signal;
  hasLastHeartbeatValue(): boolean { if (this.readError) throw this.readError; return this.marker !== undefined; }
  getLastHeartbeatValue(): unknown { return this.marker; }
  async recordHeartbeat(value: unknown): Promise<void> { this.marker = value as { state?: string }; }
}

const callId = '6f1c2a9e-0000-4000-8000-000000000001';
const balanceInput = { statement: 'UPDATE dbo.balances SET amount = amount + @p1 WHERE id = @p2', parameters: ['25.00', 7] };
const writeCall = (operationId: string, context: AttemptContext): Call => ({ id: callId, idempotencyKey: callId, connection: { provider: 'microsoft-sql-server', name: 'test' }, operation: { connectorId: 'microsoft-sql-server', operationId }, context } as unknown as Call);
const serverError = (number: number, severity: number) => Object.assign(new Error('secret server text'), { number, class: severity, state: 1 });

/** Fakes one SQL Server write session: the statement reports rowsAffected, then COMMIT runs `commit`. */
function fakeWriteSession(options: { statementError?: unknown; rowsAffected?: number; commit?: () => Promise<void> } = {}) {
  const client = new SQLServerClient({ host: '127.0.0.1', database: 'master', user: 'sa', encrypt: 'disable' }, { resolve: async () => ({ password: new SecretString('inert-test-password') }) });
  const counts = { opens: 0, commits: 0, statements: 0 };
  vi.spyOn(client, 'open').mockImplementation(async () => { counts.opens++; return { connected: true, close: async () => undefined } as unknown as sql.ConnectionPool; });
  vi.spyOn(client, 'startResponseBudget').mockReturnValue({ arm: () => undefined, close: () => undefined, hasExceeded: false } as never);
  vi.spyOn(client, 'pinSession').mockResolvedValue();
  vi.spyOn(sql.Transaction.prototype, 'begin').mockResolvedValue(undefined as never);
  vi.spyOn(sql.Transaction.prototype, 'rollback').mockResolvedValue(undefined as never);
  vi.spyOn(sql.Transaction.prototype, 'commit').mockImplementation(async () => { counts.commits++; await options.commit?.(); });
  vi.spyOn(sql.Transaction.prototype, 'request').mockImplementation(() => {
    const request = Object.assign(new EventEmitter(), {
      stream: false, arrayRowMode: false, _setCurrentRequest: () => undefined,
      input: () => request,
      query: async () => {
        counts.statements++;
        if (options.statementError) throw options.statementError;
        request.emit('rowsaffected', options.rowsAffected ?? 1);
      },
    });
    return request as unknown as sql.Request;
  });
  return { client, counts };
}

afterEach(() => { vi.restoreAllMocks(); });

describe('SQL Server writes', () => {
  it('declares the safe execute tier and the unsafe sync executeNonIdempotent tier', () => {
    expect(ExecuteStatementDefinition.repeatOnUnknown).toBe('safe');
    expect(ExecuteStatementNonIdempotentDefinition).toMatchObject({ repeatOnUnknown: 'unsafe', stepDefaults: { executeDurability: 'sync' } });
  });

  it('retries an execute COMMIT whose outcome is unknown because execute is repeat-safe', async () => {
    const { client } = fakeWriteSession({ commit: async () => { throw Object.assign(new Error('reset'), { code: 'ECONNRESET' }); } });
    expect(await client.execute().invoke(writeCall('execute', new AttemptContext()), balanceInput)).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT', operation: 'execute' } });
  });

  it('claims before COMMIT and commits once', async () => {
    const { client, counts } = fakeWriteSession();
    const context = new AttemptContext();
    expect(await client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', context), balanceInput)).toMatchObject({ kind: 'branch', branch: 'completed', value: { rowsAffected: 1 } });
    expect(context.marker?.state).toBe('claimed');
    expect(counts.commits).toBe(1);
  });

  it.each([
    ['connection lost', Object.assign(new Error('reset'), { code: 'ECONNRESET' })],
    ['server error while committing', serverError(3999, 17)],
    ['driver AggregateError while committing', new AggregateError([Object.assign(new Error('reset'), { code: 'ECONNRESET' })], 'driver failure')],
  ])('selects uncertain after a %s and never runs the statement again', async (_name, commitError) => {
    const { client, counts } = fakeWriteSession({ commit: async () => { throw commitError; } });
    const context = new AttemptContext();
    expect(await client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', context), balanceInput)).toMatchObject({ kind: 'uncertain', failure: { operation: 'executeNonIdempotent' } });
    context.attempt = 2;
    const opens = counts.opens;
    expect(await client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', context), balanceInput)).toMatchObject({ kind: 'uncertain' });
    expect(counts.opens).toBe(opens);
  });

  it('resends only after a proven rollback', async () => {
    let shouldFail = true;
    const { client, counts } = fakeWriteSession({ commit: async () => { if (shouldFail) throw serverError(1205, 13); } });
    const context = new AttemptContext();
    expect(await client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', context), balanceInput)).toMatchObject({ kind: 'retry' });
    expect(context.marker?.state).toBe('canResend');
    shouldFail = false;
    context.attempt = 2;
    expect(await client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', context), balanceInput)).toMatchObject({ kind: 'branch', branch: 'completed' });
    expect(counts.commits).toBe(2);
  });

  it('never records notSent after a possible earlier COMMIT', async () => {
    const lost = fakeWriteSession({ statementError: Object.assign(new Error('socket closed'), { code: 'ECONNRESET' }) });
    const context = new AttemptContext();
    // A canResend marker that still counts one possibly effective send keeps a send allowed.
    const seeded = { callId, attempt: 2, state: 'canResend', sendCount: 1 };
    context.marker = seeded;
    context.attempt = 3;
    // A transient failure returns Retry; the attempt after it still treats the earlier send as possible.
    expect(await lost.client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', context), balanceInput)).toMatchObject({ kind: 'retry' });
    expect(lost.counts.commits).toBe(0);
    expect(context.marker).toBe(seeded);
    context.attempt = 4;
    const opensBefore = lost.counts.opens;
    expect(await lost.client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', context), balanceInput)).toMatchObject({ kind: 'uncertain' });
    expect(lost.counts.opens).toBe(opensBefore);

    const refused = fakeWriteSession({ statementError: serverError(547, 16) });
    const refusedContext = new AttemptContext();
    refusedContext.marker = seeded;
    refusedContext.attempt = 3;
    expect(await refused.client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', refusedContext), balanceInput)).toMatchObject({ kind: 'uncertain' });
    expect(refused.counts.commits).toBe(0);
    expect(refusedContext.marker).toBe(seeded);
  });

  it('keeps a committed result when closing the pool fails afterwards', async () => {
    const { client, counts } = fakeWriteSession();
    vi.spyOn(client, 'open').mockImplementation(async () => { counts.opens++; return { connected: true, close: async () => { throw new Error('pool close failed'); } } as unknown as sql.ConnectionPool; });
    const context = new AttemptContext();
    expect(await client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', context), balanceInput)).toMatchObject({ kind: 'branch', branch: 'completed' });
    expect(counts.commits).toBe(1);
    expect(context.marker?.state).toBe('claimed');
  });

  it('returns Retry and records notSent when the credential broker is temporarily unavailable', async () => {
    const unavailable = new SQLServerClient({ host: '127.0.0.1', database: 'master', user: 'sa', encrypt: 'disable' }, { resolve: async () => { throw new CredentialBrokerUnavailableError(); } });
    const open = vi.spyOn(unavailable, 'open');
    expect(await unavailable.query().invoke(writeCall('query', new AttemptContext()), { statement: 'SELECT 1 AS one' })).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(await unavailable.execute().invoke(writeCall('execute', new AttemptContext()), balanceInput)).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    const context = new AttemptContext();
    expect(await unavailable.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', context), balanceInput)).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(context.marker?.state).toBe('notSent');
    expect(open).not.toHaveBeenCalled();
  });

  it.each([
    ['reauthorization required', new ReauthorizationRequiredError(), true],
    ['connection object not found', new ObjectNotFoundError(), true],
    ['connection not selected', new ConnectionNotSelectedError(), true],
    ['credential broker rejected', new CredentialBrokerRejectedError(), true],
    ['credential broker unavailable', new CredentialBrokerUnavailableError(), false],
    ['static connection not configured', new Error('connection is not configured'), false],
    ['rejected credential not refreshed', new RejectedCredentialNotRefreshedError(), false],
    ['unlisted error', new TypeError('unlisted'), false],
  ])('classifies a %s credential failure before and after a possible COMMIT', async (_name, credentialFailure, isTerminal) => {
    const failing = new SQLServerClient({ host: '127.0.0.1', database: 'master', user: 'sa', encrypt: 'disable' }, { resolve: async () => { throw credentialFailure; } });
    const open = vi.spyOn(failing, 'open');
    const firstExpected = isTerminal ? { kind: 'branch', branch: 'defect' } : { kind: 'retry', failure: { kind: 'AVAILABILITY' } };
    expect(await failing.execute().invoke(writeCall('execute', new AttemptContext()), balanceInput)).toMatchObject(firstExpected);
    const context = new AttemptContext();
    expect(await failing.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', context), balanceInput)).toMatchObject(firstExpected);
    expect(context.marker?.state).toBe('notSent');
    const later = new AttemptContext();
    later.marker = { callId, attempt: 2, state: 'canResend', sendCount: 1 } as { state?: string };
    later.attempt = 3;
    expect(await failing.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', later), balanceInput)).toMatchObject({ kind: isTerminal ? 'uncertain' : 'retry' });
    expect(open).not.toHaveBeenCalled();
  });

  it('selects uncertain after a lost Worker and retries an unreadable checkpoint without connecting', async () => {
    const { client, counts } = fakeWriteSession();
    const afterLostWorker = new AttemptContext();
    afterLostWorker.attempt = 2;
    expect(await client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', afterLostWorker), balanceInput)).toMatchObject({ kind: 'uncertain' });
    const unreadable = new AttemptContext();
    unreadable.readError = new Error('heartbeat store unavailable');
    expect(await client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', unreadable), balanceInput)).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(counts.opens).toBe(0);
  });

  it('rejects a definite refusal without committing', async () => {
    const rejected = fakeWriteSession({ statementError: serverError(547, 16) });
    const context = new AttemptContext();
    const result = await rejected.client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', context), balanceInput);
    expect(result).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'CONFLICT' } });
    expect(JSON.stringify(result)).not.toContain('secret');
    expect(rejected.counts.commits).toBe(0);
    expect(context.marker?.state).toBe('notSent');
    vi.restoreAllMocks();

    const noTransaction = fakeWriteSession({ commit: async () => { throw serverError(3902, 16); } });
    const atCommit = new AttemptContext();
    expect(await noTransaction.client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', atCommit), balanceInput)).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
    expect(atCommit.marker?.state).toBe('canResend');
    vi.restoreAllMocks();

    const limited = fakeWriteSession({ rowsAffected: 3 });
    expect(await limited.client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', new AttemptContext()), { ...balanceInput, maxRowsAffected: 1 })).toMatchObject({ kind: 'branch', branch: 'limitExceeded' });
    expect(limited.counts.commits).toBe(0);
    expect(await limited.client.executeNonIdempotent().invoke(writeCall('executeNonIdempotent', new AttemptContext()), { statement: 'COMMIT' })).toMatchObject({ kind: 'branch', branch: 'defect' });
  });
});
