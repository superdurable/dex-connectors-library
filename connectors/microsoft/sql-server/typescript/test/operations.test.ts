// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { EventEmitter } from 'node:events';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { checkServerIdentity, rootCertificates } from 'node:tls';
import sql from 'mssql';
import { ConnectionError as TediousConnectionError, RequestError as TediousRequestError } from 'tedious';
import { SecretString } from '@superdurable/connectors-sdk';
import { SQLServerClient, classifyDriverError } from '../src/client.js';

const client = new SQLServerClient({ host: '127.0.0.1', database: 'master', user: 'sa', encrypt: 'disable' }, {
  resolve: async () => ({ password: new SecretString('inert-test-password') }),
});

describe('Go-compatible SQL Server error decisions', () => {
  const cases = [
    [18456, 'rejected', 'AUTHENTICATION'], [18487, 'rejected', 'AUTHENTICATION'], [40615, 'rejected', 'AUTHENTICATION'],
    [4060, 'rejected', 'NOT_FOUND'], [229, 'rejected', 'AUTHORIZATION'], [3906, 'rejected', 'AUTHORIZATION'],
    [208, 'rejected', 'NOT_FOUND'], [2627, 'rejected', 'CONFLICT'], [2601, 'rejected', 'CONFLICT'],
    [547, 'rejected', 'CONFLICT'], [515, 'rejected', 'CONFLICT'], [2628, 'rejected', 'VALIDATION'],
    [245, 'rejected', 'VALIDATION'], [334, 'rejected', 'VALIDATION'], [40544, 'rejected', 'QUOTA_EXHAUSTED'],
    [40549, 'rejected', 'AVAILABILITY'], [1205, 'retry', 'CONFLICT'], [1222, 'retry', 'CONFLICT'],
    [3960, 'retry', 'CONFLICT'], [9002, 'retry', 'AVAILABILITY'], [40501, 'retry', 'AVAILABILITY'],
    [40197, 'retry', 'AVAILABILITY'], [40613, 'retry', 'AVAILABILITY'], [50999, 'retry', 'AVAILABILITY'],
    [50000, 'rejected', 'PROVIDER_REJECTION'],
  ] as const;
  for (const [number, disposition, kind] of cases) {
    it(`classifies server error ${number}`, () => {
      const actual = classifyDriverError({ number, class: number === 50999 ? 21 : 16, state: 3, message: 'secret-card-4242' }, 'statement');
      expect(actual.disposition).toBe(disposition);
      expect(actual.kind).toBe(kind);
      expect(actual.message).not.toContain('secret-card-4242');
    });
  }
  it('treats only an unconfirmed COMMIT as uncertain', () => {
    expect(classifyDriverError({ code: 'ECONNRESET' }, 'statement').disposition).toBe('retry');
    expect(classifyDriverError({ code: 'ECONNRESET' }, 'commit').disposition).toBe('uncertain');
    expect(classifyDriverError({ number: 3999, class: 17 }, 'commit').disposition).toBe('uncertain');
    expect(classifyDriverError({ number: 3902, class: 16 }, 'commit').disposition).toBe('rejected');
    expect(classifyDriverError({ number: 1205, class: 13 }, 'commit').disposition).toBe('retry');
  });
  it('separates credential, DNS, and transient dial failures', () => {
    const wrapped = (code: string, causeCode?: string) => new sql.ConnectionError(new TediousConnectionError('secret-card-4242', code, causeCode ? { cause: Object.assign(new Error('secret'), { code: causeCode }) } : undefined));
    expect(classifyDriverError(wrapped('ELOGIN'), 'connect').kind).toBe('AUTHENTICATION');
    expect(classifyDriverError(wrapped('ESOCKET', 'ENOTFOUND'), 'connect')).toMatchObject({ disposition: 'rejected', kind: 'NOT_FOUND' });
    expect(classifyDriverError(wrapped('ESOCKET', 'ERR_TLS_CERT_ALTNAME_INVALID'), 'connect')).toMatchObject({ disposition: 'rejected', kind: 'AUTHENTICATION' });
    expect(classifyDriverError(wrapped('ESOCKET', 'UNABLE_TO_VERIFY_LEAF_SIGNATURE'), 'connect').disposition).toBe('rejected');
    expect(classifyDriverError(wrapped('ESOCKET'), 'connect').disposition).toBe('retry');
    expect(classifyDriverError(wrapped('ELOGIN'), 'connect', { number: 40613, class: 20, state: 1 })).toMatchObject({ disposition: 'retry', kind: 'AVAILABILITY' });
    expect(classifyDriverError(wrapped('ELOGIN'), 'connect', { number: 4060, class: 16, state: 1 })).toMatchObject({ disposition: 'rejected', kind: 'NOT_FOUND' });
    expect(classifyDriverError(wrapped('ELOGIN'), 'connect', { number: 18456, class: 14, state: 1 })).toMatchObject({ disposition: 'rejected', kind: 'AUTHENTICATION' });
  });
  it('classifies real request wrapper codes and mapped severe errors', () => {
    const requestError = (code: string) => new sql.RequestError(new TediousRequestError('secret-card-4242', code));
    expect(classifyDriverError(requestError('ECLOSE'), 'statement')).toMatchObject({ disposition: 'retry', kind: 'TRANSPORT' });
    expect(classifyDriverError(requestError('ETIMEOUT'), 'statement')).toMatchObject({ disposition: 'rejected', kind: 'AVAILABILITY' });
    expect(classifyDriverError(requestError('ECANCEL'), 'statement').disposition).toBe('retry');
    expect(classifyDriverError(new sql.TransactionError('not begun', 'ENOTBEGUN'), 'commit')).toMatchObject({ disposition: 'retry', kind: 'TRANSPORT' });
    expect(classifyDriverError({ number: 40549, class: 21, state: 2 }, 'statement')).toMatchObject({ disposition: 'rejected', kind: 'AVAILABILITY' });
    expect(classifyDriverError({ number: 18456, class: 21, state: 2 }, 'statement')).toMatchObject({ disposition: 'rejected', kind: 'AUTHENTICATION' });
    const serverReply = (number: number, severity: number) => {
      const tediousError = Object.assign(new TediousRequestError('secret-card-4242', 'EREQUEST'), { info: { number, class: severity, state: 2 } });
      return new sql.RequestError(tediousError, 'EREQUEST');
    };
    expect(classifyDriverError(serverReply(1205, 13), 'statement')).toMatchObject({ disposition: 'retry', kind: 'CONFLICT', number: 1205 });
    expect(classifyDriverError(serverReply(2627, 16), 'statement')).toMatchObject({ disposition: 'rejected', kind: 'CONFLICT', number: 2627 });
    expect(classifyDriverError(serverReply(40549, 21), 'statement')).toMatchObject({ disposition: 'rejected', kind: 'AVAILABILITY', number: 40549 });
  });
  it('uses Node TLS SAN verification with case-insensitive DNS and IP SANs', () => {
    const certificate = { subject: { CN: 'other.example' }, subjectaltname: 'DNS:db.example, IP Address:127.0.0.1' } as Parameters<typeof checkServerIdentity>[1];
    expect(checkServerIdentity('DB.EXAMPLE', certificate)).toBeUndefined();
    expect(checkServerIdentity('127.0.0.1', certificate)).toBeUndefined();
    expect((checkServerIdentity('other.example', certificate) as Error & { code: string }).code).toBe('ERR_TLS_CERT_ALTNAME_INVALID');
  });
});

it('rejects unsafe configuration before opening a socket', () => {
  expect(() => new SQLServerClient({ host: 'db.example', database: 'master', user: 'sa', encrypt: 'disable' }, { resolve: async () => ({ password: new SecretString('x') }) })).toThrow('loopback');
  expect(client.query().definition().operation.operationId).toBe('query');
  expect(client.execute().definition().operation.operationId).toBe('execute');
  expect(() => new SQLServerClient({ host: '::1', database: 'master', user: 'sa', encrypt: 'disable' }, { resolve: async () => ({ password: new SecretString('x') }) })).not.toThrow();
  expect(() => new SQLServerClient({ host: '127.0.0.2', database: 'master', user: 'sa', encrypt: 'disable' }, { resolve: async () => ({ password: new SecretString('x') }) })).not.toThrow();
  expect(() => new SQLServerClient({ host: 'db.example', database: 'master', user: 'sa', maxRows: 100001 }, { resolve: async () => ({ password: new SecretString('x') }) })).toThrow('bounds');
  expect(() => new SQLServerClient({ host: 'db.example', database: 'master', user: 'sa', maxResponseBytes: 16 * 1048576 + 1 }, { resolve: async () => ({ password: new SecretString('x') }) })).toThrow('bounds');
  expect(new SQLServerClient({ host: 'db.example', database: 'master', user: 'sa', maxRows: 0 }, { resolve: async () => ({ password: new SecretString('x') }) }).config.maxRows).toBe(1000);
});

it('rejects an unreadable SQLSERVER_SSL_CA bundle as a configuration defect', async () => {
  const previous = process.env.SQLSERVER_SSL_CA;
  process.env.SQLSERVER_SSL_CA = '/path/that/does/not/exist/sqlserver-ca.pem';
  try {
    const secured = new SQLServerClient({ host: 'localhost', database: 'master', user: 'sa', encrypt: 'mandatory' }, { resolve: async () => ({ password: new SecretString('test-password') }) });
    const call = { id: 'test-call', context: { cancellationSignal: new AbortController().signal } } as Parameters<SQLServerClient['open']>[0];
    const failure = await secured.open(call).catch(error => error as Error);
    expect(failure).toBeInstanceOf(Error);
    expect(classifyDriverError(failure, 'connect')).toMatchObject({ disposition: 'defect', kind: 'VALIDATION' });
    expect((failure as Error).message).not.toContain('test-password');
  } finally {
    if (previous === undefined) delete process.env.SQLSERVER_SSL_CA;
    else process.env.SQLSERVER_SSL_CA = previous;
  }
});

it('captures a real login token and loads a private CA on every connect', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'sql-server-ca-test-'));
  const bundle = join(directory, 'root.pem');
  await writeFile(bundle, rootCertificates[0]!);
  const previousCA = process.env.SQLSERVER_SSL_CA;
  const originalPool = sql.ConnectionPool;
  let capturedConfiguration: sql.config | undefined;
  class TestPool {
    constructor(configuration: sql.config) { capturedConfiguration = configuration; }
    async connect(): Promise<never> {
      const connection = new EventEmitter();
      capturedConfiguration?.beforeConnect?.(connection as Parameters<NonNullable<sql.config['beforeConnect']>>[0]);
      connection.emit('errorMessage', { number: 40613, class: 20, state: 1, message: 'secret-card-4242' });
      throw new sql.ConnectionError(new TediousConnectionError('secret-card-4242', 'ELOGIN'));
    }
  }
  (sql as { ConnectionPool: typeof sql.ConnectionPool }).ConnectionPool = TestPool as unknown as typeof sql.ConnectionPool;
  process.env.SQLSERVER_SSL_CA = bundle;
  try {
    const secured = new SQLServerClient({ host: 'localhost', database: 'master', user: 'sa', encrypt: 'mandatory' }, { resolve: async () => ({ password: new SecretString('test-password') }) });
    const call = { id: 'test-call', context: { cancellationSignal: new AbortController().signal } } as Parameters<SQLServerClient['open']>[0];
    const failure = await secured.open(call).catch(error => error as Error);
    expect(classifyDriverError(failure, 'connect')).toMatchObject({ disposition: 'retry', kind: 'AVAILABILITY', number: 40613 });
    expect(capturedConfiguration?.options?.maxRetriesOnTransientErrors).toBe(0);
    expect(capturedConfiguration?.options?.cryptoCredentialsDetails?.ca).toEqual(Buffer.from(rootCertificates[0]!));
    expect(classifyDriverError(failure, 'connect').message).not.toContain('secret-card-4242');
    await writeFile(bundle, rootCertificates[1]!);
    const strict = new SQLServerClient({ host: 'localhost', database: 'master', user: 'sa', encrypt: 'strict' }, { resolve: async () => ({ password: new SecretString('test-password') }) });
    await strict.open(call).catch(() => undefined);
    expect(capturedConfiguration?.options?.encrypt).toBe('strict');
    expect(capturedConfiguration?.options?.cryptoCredentialsDetails?.ca).toEqual(Buffer.from(rootCertificates[1]!));
  } finally {
    (sql as { ConnectionPool: typeof sql.ConnectionPool }).ConnectionPool = originalPool;
    if (previousCA === undefined) delete process.env.SQLSERVER_SSL_CA;
    else process.env.SQLSERVER_SSL_CA = previousCA;
    await rm(directory, { recursive: true, force: true });
  }
});
