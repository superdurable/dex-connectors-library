// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { X509Certificate } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { isIP } from 'node:net';
import type { Socket } from 'node:net';
import { createSecureContext, type TLSSocket } from 'node:tls';
import sql from 'mssql';
import { SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { QueryRowsOperation, ExecuteStatementOperation } from './operations.js';

type Phase = 'connect' | 'begin' | 'statement' | 'commit';
export type Disposition = 'retry' | 'rejected' | 'uncertain' | 'invalidResponse' | 'defect';
export interface ClassifiedFailure { disposition: Disposition; kind: Failure['kind']; message: string; number?: number; state?: number; severity?: number; }
interface ServerErrorToken { number: number; class: number; state: number; }
const connectionTokens = new WeakMap<object, ServerErrorToken>();
const sessionIdentities = new WeakMap<sql.ConnectionPool, Record<string, string>>();
const poolConnections = new WeakMap<sql.ConnectionPool, { socket?: Socket }>();
const poolErrors = new WeakMap<sql.ConnectionPool, unknown>();

function validHostname(host: string, isPattern: boolean): boolean {
  if (!isPattern) host = host.replace(/\.$/, '');
  if (!host || host === '*') return false;
  return host.split('.').every((label, index) => label === '*' && isPattern && index === 0 || !!label && [...label].every((character, position) => /[a-z0-9_]/i.test(character) || character === '-' && position !== 0));
}

/** Match only DNS or IP subject alternative names using Go crypto/x509 hostname rules. */
export function certificateMatchesHost(host: string, certificate: X509Certificate): boolean {
  const names = certificate.subjectAltName?.split(/, (?=(?:DNS|IP Address):)/) ?? [];
  if (isIP(host)) return certificate.checkIP(host) !== undefined;
  const candidate = host.toLowerCase();
  const parts = candidate.replace(/\.$/, '').split('.');
  return names.some(name => {
    if (!name.startsWith('DNS:')) return false;
    const pattern = name.slice(4).toLowerCase();
    if (validHostname(candidate, false) && validHostname(pattern, true)) {
      const labels = pattern.split('.');
      return labels.length === parts.length && labels.every((label, index) => index === 0 && label === '*' || label === parts[index]);
    }
    return pattern === candidate && pattern !== '.';
  });
}

function certificateError(): Error & { code: string } {
  return Object.assign(new Error('the SQL Server certificate failed encrypt verification'), { code: 'ERR_TLS_CERT_ALTNAME_INVALID' });
}

function verifyMandatoryPeer(tlsSocket: TLSSocket, host: string): boolean {
  try {
    const peer = tlsSocket.getPeerCertificate(true);
    const certificate = peer.raw && new X509Certificate(peer.raw);
    const authorizationError: unknown = tlsSocket.authorizationError;
    const authorizationCode = typeof authorizationError === 'string' ? authorizationError : (authorizationError as { code?: string } | undefined)?.code;
    if (certificate && (tlsSocket.authorized || authorizationCode === 'ERR_TLS_CERT_ALTNAME_INVALID') && certificateMatchesHost(host, certificate)) return true;
  } catch { /* Malformed peer certificates are authentication failures. */ }
  tlsSocket.destroy(certificateError());
  return false;
}

/** Track server bytes before Tedious materializes MAX values or JSON chunks. */
export class ResponseBudget {
  private bytes = 0;
  hasExceeded = false;
  constructor(private readonly socket: Socket, private readonly limit: number) { socket.on('data', this.countBytes); }
  private readonly countBytes = (chunk: Buffer): void => {
    this.bytes += chunk.length;
    if (this.bytes > this.limit + 65536) { this.hasExceeded = true; this.socket.destroy(Object.assign(new Error('SQL Server response exceeded its wire budget'), { code: 'ERESPONSETOOLARGE' })); }
  };
  close(): void { this.socket.off('data', this.countBytes); }
}

function isLoopbackHost(host: string): boolean {
  if (host === 'localhost') return true;
  if (isIP(host) === 4) return host.split('.')[0] === '127';
  return isIP(host) === 6 && (host === '::1' || /^0(?::0){6}:1$/.test(host) || /^::ffff:127\./i.test(host));
}

/** A local connection configuration failure, with no provider supplied text. */
export class ConnectionConfigurationError extends Error {}
/** Strict TDS 8 TLS cannot be verified before LOGIN7 through Tedious's connector API. */
export class UnsupportedStrictVerificationError extends Error {}
const numberGroups: Array<[Failure['kind'], number[]]> = [
  ['CONFLICT', [1205, 1222, 3960, 3961, 3902, 3903, 515, 547, 2601, 2627]],
  ['AVAILABILITY', [615, 701, 926, 1101, 1105, 4221, 6005, 8628, 8645, 8651, 9002, 10922, 10928, 10929, 10936, 18401, 40197, 40501, 40613, 49918, 49919, 49920, 40549, 40550, 40551, 40552, 40553]],
  ['AUTHENTICATION', [18452, 18456, 18470, 18486, 18487, 18488, 40532, 40615]],
  ['AUTHORIZATION', [229, 230, 262, 297, 916, 3906]],
  ['NOT_FOUND', [207, 208, 2812, 4060, 4064, 4104]],
  ['VALIDATION', [102, 137, 156, 241, 242, 245, 266, 334, 574, 2628, 8003, 8114, 8115, 8134, 8152]],
  ['QUOTA_EXHAUSTED', [40544]],
];
const retryNumbers = new Set([1205, 1222, 3960, 3961, 615, 701, 926, 1101, 1105, 4221, 6005, 8628, 8645, 8651, 9002, 10922, 10928, 10929, 10936, 18401, 40197, 40501, 40613, 49918, 49919, 49920]);

/** Classify a Tedious driver error without reflecting its possibly secret message. */
export function classifyDriverError(error: unknown, phase: Phase, capturedToken?: ServerErrorToken): ClassifiedFailure {
  if (error instanceof ConnectionConfigurationError) return { disposition: 'defect', kind: 'VALIDATION', message: error.message };
  if (error instanceof UnsupportedStrictVerificationError) return { disposition: 'rejected', kind: 'AUTHENTICATION', message: error.message };
  const driverError = error as { number?: number; class?: number; state?: number; code?: string; message?: string; originalError?: { info?: ServerErrorToken; code?: string; message?: string; cause?: { code?: string } }; cause?: { code?: string } };
  const info = capturedToken ?? (error && typeof error === 'object' ? connectionTokens.get(error) : undefined) ?? driverError?.originalError?.info;
  const number = info?.number ?? driverError?.number;
  if (typeof number === 'number') {
    const severity = info?.class ?? driverError.class ?? 0;
    const state = info?.state ?? driverError.state ?? 0;
    if (phase === 'commit' && number !== 1205 && number !== 3902 && number !== 3903)
      return { disposition: 'uncertain', kind: 'AVAILABILITY', message: `the server returned error ${number}, severity ${severity}, state ${state} while committing, so whether the transaction committed is unknown` };
    const mappedKind = numberGroups.find(([, numbers]) => numbers.includes(number))?.[0];
    const kind = mappedKind ?? (severity >= 20 ? 'AVAILABILITY' : 'PROVIDER_REJECTION');
    const disposition = retryNumbers.has(number) || !mappedKind && severity >= 20 ? 'retry' : 'rejected';
    return { disposition, kind, number, state, severity, message: `the server returned error ${number}, severity ${severity}, state ${state} during ${phase}` };
  }
  const nestedCode = driverError?.originalError?.cause?.code ?? driverError?.originalError?.code ?? driverError?.cause?.code;
  const certificateCodes = new Set(['ERR_TLS_CERT_ALTNAME_INVALID', 'UNABLE_TO_VERIFY_LEAF_SIGNATURE', 'SELF_SIGNED_CERT_IN_CHAIN', 'DEPTH_ZERO_SELF_SIGNED_CERT', 'UNABLE_TO_GET_ISSUER_CERT_LOCALLY', 'CERT_HAS_EXPIRED', 'CERT_NOT_YET_VALID']);
  const code = nestedCode && (nestedCode === 'ENOTFOUND' || certificateCodes.has(nestedCode)) ? nestedCode : driverError?.code ?? nestedCode;
  if (phase === 'connect') {
    if (String(driverError?.originalError?.message ?? driverError?.message ?? '').includes('server does not support encryption')) return { disposition: 'rejected', kind: 'PROVIDER_REJECTION', message: 'the SQL Server does not accept TLS, which encrypt requires' };
    if (code === 'ENOTFOUND') return { disposition: 'rejected', kind: 'NOT_FOUND', message: 'the SQL Server host name does not resolve' };
    if (code === 'ELOGIN') return { disposition: 'rejected', kind: 'AUTHENTICATION', message: 'the SQL Server rejected the login' };
    if (code && certificateCodes.has(code))
      return { disposition: 'rejected', kind: 'AUTHENTICATION', message: 'the SQL Server certificate failed encrypt verification' };
    return { disposition: 'retry', kind: 'AVAILABILITY', message: 'the SQL Server is unreachable or did not finish connecting in time' };
  }
  if (phase === 'commit' && code === 'ENOTBEGUN') return { disposition: 'retry', kind: 'TRANSPORT', message: 'COMMIT was not sent because the transaction never began' };
  if (phase === 'commit') return { disposition: 'uncertain', kind: 'TRANSPORT', message: 'the connection failed after COMMIT was sent, so whether the transaction committed is unknown' };
  if (code === 'ETIMEOUT' && phase === 'statement') return { disposition: 'rejected', kind: 'AVAILABILITY', message: 'the statement exceeded statementTimeout, so the connector canceled it; nothing was committed' };
  if (code === 'ETIMEOUT' && phase === 'begin') return { disposition: 'retry', kind: 'TRANSPORT', message: 'the connection timed out while starting the transaction' };
  if (['ESOCKET', 'ECONNCLOSED', 'ECONNRESET', 'EPIPE', 'ECONNREFUSED', 'ENOTOPEN', 'ECLOSE', 'EINVALIDSTATE', 'ECANCEL'].includes(code ?? ''))
    return { disposition: 'retry', kind: 'TRANSPORT', message: `the connection failed during ${phase} before anything was committed` };
  return { disposition: 'invalidResponse', kind: 'PROTOCOL', message: `the driver could not decode the server reply during ${phase}; nothing was committed` };
}

/** A single SQL Server client; each operation opens and closes its own one-connection pool. */
export class SQLServerClient {
  readonly config: Required<Config>;
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, private readonly now: () => Date = () => new Date()) {
    if (!credentials) throw new TypeError('SQL Server credential provider is required');
    if (!config.host || config.host.trim() !== config.host || /[,;\s'"=\\\p{Cc}]/u.test(config.host) || config.host.includes('://') || /^tcp:/i.test(config.host) || config.host.includes(':') && !isIP(config.host)) throw new TypeError('SQL Server host must be one DNS name or IP address');
    if (!config.database?.trim() || !config.user?.trim() || config.user.includes('\\') || [config.database, config.user].some(value => [...value].length > 128 || /\p{Cc}/u.test(value))) throw new TypeError('SQL Server database and SQL authentication user are required');
    this.config = { port: 1433, encrypt: 'mandatory', connectTimeoutMs: 10000, statementTimeoutMs: 5000, maxRows: 1000, maxResponseBytes: 1048576, ...Object.fromEntries(Object.entries(config).filter(([, value]) => value !== 0 && value !== undefined)) } as Required<Config>;
    if (!Number.isSafeInteger(this.config.port) || this.config.port < 1 || this.config.port > 65535) throw new TypeError('invalid SQL Server port');
    if (!['mandatory', 'strict', 'trust-server-certificate', 'disable'].includes(this.config.encrypt)) throw new TypeError('invalid SQL Server encrypt policy');
    if (this.config.encrypt === 'disable' && !isLoopbackHost(this.config.host)) throw new TypeError('SQL Server encryption may be disabled only for loopback');
    if (!Number.isSafeInteger(this.config.statementTimeoutMs) || this.config.statementTimeoutMs < 1 || this.config.statementTimeoutMs > 2147483647 || !Number.isSafeInteger(this.config.connectTimeoutMs) || this.config.connectTimeoutMs < 1 || !Number.isSafeInteger(this.config.maxRows) || this.config.maxRows < 1 || this.config.maxRows > 100000 || !Number.isSafeInteger(this.config.maxResponseBytes) || this.config.maxResponseBytes < 1 || this.config.maxResponseBytes > 16 * 1048576) throw new TypeError('invalid SQL Server bounds');
  }
  /** Bind the read operation. */ query(): QueryRowsOperation { return new QueryRowsOperation(this); }
  /** Bind the write operation. */ execute(): ExecuteStatementOperation { return new ExecuteStatementOperation(this); }
  /** Build a receipt without including connection settings or credentials. */
  receipt(call: Call, pool?: sql.ConnectionPool, classification?: ClassifiedFailure): ReceiptDraft {
    const metadata = { ...(pool ? sessionIdentities.get(pool) : {}), ...(classification?.number ? { errorNumber: String(classification.number), errorState: String(classification.state), errorSeverity: String(classification.severity) } : {}) };
    return { provider: 'microsoft-sql-server', callId: call.id, observedAt: this.now().toISOString(), ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), ...(Object.keys(metadata).length ? { metadata } : {}) };
  }
  /** Pin SQL settings and record safe server identity on the transaction's connection. */
  async pinSession(transaction: sql.Transaction, pool: sql.ConnectionPool): Promise<void> {
    const batch = `SET LANGUAGE us_english; SET DATEFORMAT ymd; SET XACT_ABORT ON; SET NOCOUNT OFF; SET IMPLICIT_TRANSACTIONS OFF; SET ANSI_NULLS ON; SET ANSI_PADDING ON; SET ANSI_WARNINGS ON; SET ARITHABORT ON; SET CONCAT_NULL_YIELDS_NULL ON; SET QUOTED_IDENTIFIER ON; SET NUMERIC_ROUNDABORT OFF; SET TEXTSIZE 2147483647; SET TRANSACTION ISOLATION LEVEL READ COMMITTED; SET LOCK_TIMEOUT ${this.config.statementTimeoutMs}; SELECT @@SPID AS processId, CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128)) AS serverVersion`;
    const result = await transaction.request().query<{ processId: number; serverVersion: string }>(batch);
    const identity = result.recordset?.[0];
    if (!identity || !Number.isSafeInteger(identity.processId) || identity.processId < 0 || !/^[\x20-\x7e]{1,128}$/.test(identity.serverVersion)) throw new Error('invalid SQL Server session identity');
    sessionIdentities.set(pool, { processId: String(identity.processId), serverVersion: identity.serverVersion });
  }
  /** Resolve the password for this call before entering the driver error path. */
  async resolvePassword(call: Call): Promise<string> {
    const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
    if (!(credentials.password instanceof SecretString) || !credentials.password.reveal()) throw new TypeError('SQL Server connection credentials are unavailable');
    return credentials.password.reveal();
  }
  /** Open the driver using credentials already resolved by the operation. */
  async open(call: Call, resolvedPassword?: string): Promise<sql.ConnectionPool> {
    const password = resolvedPassword ?? await this.resolvePassword(call);
    if (this.config.encrypt === 'strict') throw new UnsupportedStrictVerificationError('strict TLS hostname verification is unavailable in this Tedious version');
    let trustedRoots: Buffer | undefined;
    const caFile = process.env.SQLSERVER_SSL_CA;
    if (caFile && ['mandatory', 'strict'].includes(this.config.encrypt)) {
      try {
        trustedRoots = await readFile(caFile);
        const blocks = trustedRoots.toString('utf8').match(/-----BEGIN CERTIFICATE-----[\s\S]*?-----END CERTIFICATE-----/g);
        if (!blocks?.length) throw new Error('no PEM certificates');
        for (const block of blocks) new X509Certificate(block);
        createSecureContext({ ca: trustedRoots });
      }
      catch { throw new ConnectionConfigurationError('the SQLSERVER_SSL_CA PEM bundle could not be read or parsed'); }
    }
    let connectServerError: ServerErrorToken | undefined;
    let hasRejectedCertificate = false;
    const configuration: sql.config = {
      server: this.config.host, port: this.config.port, database: this.config.database, user: this.config.user, password,
      connectionTimeout: this.config.connectTimeoutMs, requestTimeout: this.config.statementTimeoutMs,
      pool: { min: 0, max: 1 }, beforeConnect: connection => {
        poolConnections.set(pool, connection as unknown as { socket?: Socket });
        connection.on('errorMessage', token => { connectServerError = { number: token.number, class: token.class, state: token.state }; });
        if (this.config.encrypt === 'mandatory') connection.on('secure', cleartext => { hasRejectedCertificate = !verifyMandatoryPeer(cleartext as TLSSocket, this.config.host); });
      },
      options: { encrypt: this.config.encrypt !== 'disable', trustServerCertificate: ['mandatory', 'trust-server-certificate'].includes(this.config.encrypt), enableArithAbort: true, fallbackToDefaultDb: false, lowerCaseGuids: true, useUTC: true, appName: 'dex-sql-server-connector', maxRetriesOnTransientErrors: 0, ...(trustedRoots ? { cryptoCredentialsDetails: { ca: trustedRoots } } : {}) },
    };
    const pool = new sql.ConnectionPool(configuration);
    pool.on('error', error => { poolErrors.set(pool, error); });
    try { await pool.connect(); }
    catch (error) {
      if (hasRejectedCertificate) throw certificateError();
      const failure = poolErrors.get(pool) ?? error;
      if (connectServerError && failure && typeof failure === 'object') connectionTokens.set(failure, connectServerError);
      throw failure;
    }
    return pool;
  }
  /** Bound one statement's raw reply bytes before the driver decodes rows. */
  startResponseBudget(pool: sql.ConnectionPool): ResponseBudget {
    const socket = poolConnections.get(pool)?.socket;
    if (!socket) throw new ConnectionConfigurationError('the SQL Server driver did not expose its response socket');
    return new ResponseBudget(socket, this.config.maxResponseBytes);
  }
  /** Prefer an asynchronous pool failure when the request also fails. */
  recordedPoolError(pool: sql.ConnectionPool): unknown { return poolErrors.get(pool); }
  /** Convert a classified error into the connector failure shape. */
  failure(operation: string, classification: ClassifiedFailure): Failure { return { provider: 'microsoft-sql-server', operation, kind: classification.kind, message: classification.message }; }
}
