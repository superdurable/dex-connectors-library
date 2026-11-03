// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { EventEmitter } from 'node:events';
import { Socket } from 'node:net';
import { X509Certificate } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { rootCertificates } from 'node:tls';
import sql from 'mssql';
import { ConnectionError as TediousConnectionError, RequestError as TediousRequestError } from 'tedious';
import { SecretString } from '@superdurable/connectors-sdk';
import { ResponseBudget, SQLServerClient, certificateMatchesHost, classifyDriverError } from '../src/client.js';

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
    expect(classifyDriverError({ code: 'EINVALIDSTATE' }, 'commit')).toMatchObject({ disposition: 'retry', kind: 'TRANSPORT', message: 'COMMIT was not sent because the connection closed' });
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
    expect(classifyDriverError(new sql.RequestError('timeout', 'ETIMEOUT'), 'begin')).toMatchObject({ disposition: 'retry', kind: 'TRANSPORT' });
    expect(classifyDriverError(Object.assign(new Error("Server did not negotiate encryption, but the 'encrypt' config option is set to `true`"), { code: 'EENCRYPT' }), 'connect')).toMatchObject({ disposition: 'rejected', kind: 'PROVIDER_REJECTION' });
  });
  it('matches Go hostname rules against real CN-only, wildcard, and IP-SAN certificates', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'sql-server-hostname-'));
    const issue = async (name: string, san?: string): Promise<X509Certificate> => {
      const certificatePath = join(directory, `${name}.pem`);
      execFileSync('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-keyout', join(directory, `${name}.key`), '-out', certificatePath, '-subj', '/CN=db.example.com', ...(san ? ['-addext', `subjectAltName=${san}`] : [])], { stdio: 'ignore' });
      return new X509Certificate(await readFile(certificatePath));
    };
    try {
      const commonNameOnly = await issue('common-name');
      expect(certificateMatchesHost('db.example.com', commonNameOnly)).toBe(false);
      const partialWildcard = await issue('partial-wildcard', 'DNS:db*.example.com');
      expect(certificateMatchesHost('db1.example.com', partialWildcard)).toBe(false);
      const wildcard = await issue('wildcard', 'DNS:*.com');
      expect(certificateMatchesHost('EXAMPLE.COM', wildcard)).toBe(true);
      const ipAddress = await issue('ip', 'IP:127.0.0.1,DNS:db.example.com');
      expect(certificateMatchesHost('127.0.0.1', ipAddress)).toBe(true);
      expect(certificateMatchesHost('::ffff:127.0.0.1', ipAddress)).toBe(true);
      expect(certificateMatchesHost('127.0.0.2', ipAddress)).toBe(false);
      expect(certificateMatchesHost('DB.EXAMPLE.COM', ipAddress)).toBe(true);
    } finally { await rm(directory, { recursive: true, force: true }); }
  });
});

it('cuts off the raw response before a MAX value can fill driver memory', () => {
  const socket = new Socket();
  socket.on('error', () => {});
  const budget = new ResponseBudget(socket, 2);
  socket.emit('data', Buffer.alloc(65539));
  expect(budget.hasExceeded).toBe(true);
  expect(socket.destroyed).toBe(true);
  budget.close();
});

it('rearms the wire budget for a bounded transaction control reply', () => {
  const socket = new Socket();
  socket.on('error', () => {});
  const budget = new ResponseBudget(socket, 4 * 1048576);
  budget.arm(65537);
  socket.emit('data', Buffer.alloc(100));
  budget.arm(4 * 1048576);
  expect(budget.hasExceeded).toBe(false);
  socket.emit('data', Buffer.alloc(4 * 1048576));
  expect(budget.hasExceeded).toBe(false);
  socket.emit('data', Buffer.alloc(65537));
  expect(budget.hasExceeded).toBe(true);
  budget.close();
});

it('verifies each Azure-routed TLS connection against its own server name', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'sql-server-routed-host-'));
  const issue = async (name: string): Promise<X509Certificate> => {
    const certificatePath = join(directory, `${name}.pem`);
    execFileSync('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-keyout', join(directory, `${name}.key`), '-out', certificatePath, '-subj', `/CN=${name}.example`, '-addext', `subjectAltName=DNS:${name}.example`], { stdio: 'ignore' });
    return new X509Certificate(await readFile(certificatePath));
  };
  const originalPool = sql.ConnectionPool;
  try {
    const gatewayCertificate = await issue('gateway');
    const workerCertificate = await issue('worker');
    let configuration: sql.config;
    const destroyed: boolean[] = [];
    class TestPool extends EventEmitter {
      constructor(poolConfiguration: sql.config) { super(); configuration = poolConfiguration; }
      async connect(): Promise<this> {
        const connection = new EventEmitter() as EventEmitter & { routingData?: { server: string }; config?: { options: { serverName?: string } } };
        configuration.beforeConnect?.(connection as Parameters<NonNullable<sql.config['beforeConnect']>>[0]);
        for (const [host, certificate] of [['gateway.example', gatewayCertificate], ['worker.example', workerCertificate], ['wrong.example', workerCertificate]] as const) {
          if (host === 'gateway.example') delete connection.routingData;
          else connection.routingData = { server: host };
          let wasDestroyed = false;
          connection.emit('secure', { authorized: true, getPeerCertificate: () => ({ raw: certificate.raw }), destroy: () => { wasDestroyed = true; } });
          destroyed.push(wasDestroyed);
        }
        return this;
      }
    }
    (sql as { ConnectionPool: typeof sql.ConnectionPool }).ConnectionPool = TestPool as unknown as typeof sql.ConnectionPool;
    const secured = new SQLServerClient({ host: 'gateway.example', database: 'master', user: 'sa', encrypt: 'mandatory', statementTimeoutMs: 1 }, { resolve: async () => ({ password: new SecretString('test-password') }) });
    const call = { id: 'test-call', context: { cancellationSignal: new AbortController().signal } } as Parameters<SQLServerClient['open']>[0];
    await secured.open(call);
    expect(destroyed).toEqual([false, false, true]);
    expect(configuration!.requestTimeout).toBe(5001);
  } finally {
    (sql as { ConnectionPool: typeof sql.ConnectionPool }).ConnectionPool = originalPool;
    await rm(directory, { recursive: true, force: true });
  }
});

it('records pool error events without an uncaught EventEmitter exception', async () => {
  const originalPool = sql.ConnectionPool;
  class TestPool extends EventEmitter {
    async connect(): Promise<never> { this.emit('error', Object.assign(new Error('No event in state'), { code: 'EINVALIDSTATE' })); throw new Error('request also failed'); }
  }
  (sql as { ConnectionPool: typeof sql.ConnectionPool }).ConnectionPool = TestPool as unknown as typeof sql.ConnectionPool;
  try {
    const call = { id: 'test-call', context: { cancellationSignal: new AbortController().signal } } as Parameters<SQLServerClient['open']>[0];
    const error = await client.open(call).catch(failure => failure);
    expect(classifyDriverError(error, 'connect')).toMatchObject({ disposition: 'retry', kind: 'AVAILABILITY' });
    expect((error as Error).message).toBe('No event in state');
  } finally { (sql as { ConnectionPool: typeof sql.ConnectionPool }).ConnectionPool = originalPool; }
});

it('rejects unsafe configuration before opening a socket', () => {
  expect(() => new SQLServerClient({ host: 'db.example', database: 'master', user: 'sa', encrypt: 'disable' }, { resolve: async () => ({ password: new SecretString('x') }) })).toThrow('loopback');
  expect(client.query().definition().operation.operationId).toBe('query');
  expect(client.execute().definition().operation.operationId).toBe('execute');
  expect(() => new SQLServerClient({ host: '::1', database: 'master', user: 'sa', encrypt: 'disable' }, { resolve: async () => ({ password: new SecretString('x') }) })).not.toThrow();
  expect(() => new SQLServerClient({ host: '127.0.0.2', database: 'master', user: 'sa', encrypt: 'disable' }, { resolve: async () => ({ password: new SecretString('x') }) })).not.toThrow();
  expect(() => new SQLServerClient({ host: 'db.example', database: 'master', user: 'sa', maxRows: 100001 }, { resolve: async () => ({ password: new SecretString('x') }) })).toThrow('bounds');
  expect(() => new SQLServerClient({ host: 'db.example', database: 'master', user: 'sa', maxResponseBytes: 16 * 1048576 + 1 }, { resolve: async () => ({ password: new SecretString('x') }) })).toThrow('bounds');
  expect(() => new SQLServerClient({ host: 'db.example', database: 'master', user: 'sa', encrypt: 'strict' }, { resolve: async () => ({ password: new SecretString('x') }) })).toThrow('strict TLS');
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

it('rejects a malformed SQLSERVER_SSL_CA bundle before connecting', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'sql-server-invalid-ca-'));
  const bundle = join(directory, 'root.pem');
  await writeFile(bundle, 'this is not a PEM certificate');
  const previous = process.env.SQLSERVER_SSL_CA;
  process.env.SQLSERVER_SSL_CA = bundle;
  try {
    const secured = new SQLServerClient({ host: 'localhost', database: 'master', user: 'sa' }, { resolve: async () => ({ password: new SecretString('test-password') }) });
    const call = { id: 'test-call', context: { cancellationSignal: new AbortController().signal } } as Parameters<SQLServerClient['open']>[0];
    expect(classifyDriverError(await secured.open(call).catch(error => error), 'connect')).toMatchObject({ disposition: 'defect', kind: 'VALIDATION' });
  } finally {
    if (previous === undefined) delete process.env.SQLSERVER_SSL_CA;
    else process.env.SQLSERVER_SSL_CA = previous;
    await rm(directory, { recursive: true, force: true });
  }
});

it('treats credential-provider errors as authentication defects for both operations', async () => {
  const unavailable = new SQLServerClient({ host: 'localhost', database: 'master', user: 'sa' }, { resolve: async () => { throw new Error('missing secret 4242'); } });
  const call = { id: 'test-call', context: { cancellationSignal: new AbortController().signal } } as Parameters<SQLServerClient['open']>[0];
  const query = await unavailable.query().invoke(call, { statement: 'SELECT 1 AS answer', parameters: [] });
  const execute = await unavailable.execute().invoke(call, { statement: 'UPDATE dbo.t SET value = 1', parameters: [] });
  expect(query).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
  expect(execute).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
  expect(JSON.stringify([query, execute])).not.toContain('4242');
});

it('captures a real login token and loads a private CA on every connect', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'sql-server-ca-test-'));
  const bundle = join(directory, 'root.pem');
  await writeFile(bundle, rootCertificates[0]!);
  const previousCA = process.env.SQLSERVER_SSL_CA;
  const originalPool = sql.ConnectionPool;
  let capturedConfiguration: sql.config | undefined;
  class TestPool extends EventEmitter {
    constructor(configuration: sql.config) { super(); capturedConfiguration = configuration; }
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
    expect(() => new SQLServerClient({ host: 'localhost', database: 'master', user: 'sa', encrypt: 'strict' }, { resolve: async () => ({ password: new SecretString('test-password') }) })).toThrow('strict TLS');
  } finally {
    (sql as { ConnectionPool: typeof sql.ConnectionPool }).ConnectionPool = originalPool;
    if (previousCA === undefined) delete process.env.SQLSERVER_SSL_CA;
    else process.env.SQLSERVER_SSL_CA = previousCA;
    await rm(directory, { recursive: true, force: true });
  }
});
