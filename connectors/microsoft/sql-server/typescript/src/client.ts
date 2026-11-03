// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import sql from 'mssql';
import { SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { QueryRowsOperation, ExecuteStatementOperation } from './operations.js';

type Phase = 'connect' | 'begin' | 'statement' | 'commit';
export type Disposition = 'retry' | 'rejected' | 'uncertain' | 'invalidResponse' | 'defect';
export interface ClassifiedFailure { disposition: Disposition; kind: Failure['kind']; message: string; }
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
export function classifyDriverError(error: unknown, phase: Phase): ClassifiedFailure {
  const driverError = error as { number?: number; class?: number; state?: number; code?: string; originalError?: { info?: { number?: number; class?: number; state?: number } }; cause?: { code?: string } };
  const info = driverError?.originalError?.info;
  const number = info?.number ?? driverError?.number;
  if (typeof number === 'number') {
    const severity = info?.class ?? driverError.class ?? 0;
    const state = info?.state ?? driverError.state ?? 0;
    if (phase === 'commit' && number !== 1205 && number !== 3902 && number !== 3903)
      return { disposition: 'uncertain', kind: 'AVAILABILITY', message: `the server returned error ${number}, severity ${severity}, state ${state} while committing, so whether the transaction committed is unknown` };
    const kind = numberGroups.find(([, numbers]) => numbers.includes(number))?.[0] ?? (severity >= 20 ? 'AVAILABILITY' : 'PROVIDER_REJECTION');
    const disposition = retryNumbers.has(number) || severity >= 20 && number !== 40544 ? 'retry' : 'rejected';
    return { disposition, kind, message: `the server returned error ${number}, severity ${severity}, state ${state} during ${phase}` };
  }
  const code = driverError?.code ?? driverError?.cause?.code;
  if (phase === 'connect') {
    if (code === 'ENOTFOUND') return { disposition: 'rejected', kind: 'NOT_FOUND', message: 'the SQL Server host name does not resolve' };
    if (code === 'ELOGIN') return { disposition: 'rejected', kind: 'AUTHENTICATION', message: 'the SQL Server rejected the login' };
    if (code === 'ERR_TLS_CERT_ALTNAME_INVALID' || code === 'UNABLE_TO_VERIFY_LEAF_SIGNATURE' || code === 'SELF_SIGNED_CERT_IN_CHAIN')
      return { disposition: 'rejected', kind: 'AUTHENTICATION', message: 'the SQL Server certificate failed encrypt verification' };
    return { disposition: 'retry', kind: 'AVAILABILITY', message: 'the SQL Server is unreachable or did not finish connecting in time' };
  }
  if (phase === 'commit') return { disposition: 'uncertain', kind: 'TRANSPORT', message: 'the connection failed after COMMIT was sent, so whether the transaction committed is unknown' };
  if (code === 'ETIMEOUT' || code === 'ECANCEL') return { disposition: 'rejected', kind: 'AVAILABILITY', message: 'the statement exceeded statementTimeout, so the connector canceled it; nothing was committed' };
  if (['ESOCKET', 'ECONNCLOSED', 'ECONNRESET', 'EPIPE', 'ECONNREFUSED', 'ENOTOPEN'].includes(code ?? ''))
    return { disposition: 'retry', kind: 'TRANSPORT', message: `the connection failed during ${phase} before anything was committed` };
  return { disposition: 'invalidResponse', kind: 'PROTOCOL', message: `the driver could not decode the server reply during ${phase}; nothing was committed` };
}

/** A single SQL Server client; each operation opens and closes its own one-connection pool. */
export class SQLServerClient {
  readonly config: Required<Config>;
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, private readonly now: () => Date = () => new Date()) {
    if (!credentials) throw new TypeError('SQL Server credential provider is required');
    if (!config.host || /[\\/:,\s]/.test(config.host)) throw new TypeError('SQL Server host must be one DNS name or IP address');
    if (!config.database?.trim() || !config.user?.trim() || config.user.includes('\\')) throw new TypeError('SQL Server database and SQL authentication user are required');
    this.config = { port: 1433, encrypt: 'mandatory', connectTimeoutMs: 10000, statementTimeoutMs: 5000, maxRows: 1000, maxResponseBytes: 1048576, ...config };
    if (!Number.isSafeInteger(this.config.port) || this.config.port < 1 || this.config.port > 65535) throw new TypeError('invalid SQL Server port');
    if (!['mandatory', 'strict', 'trust-server-certificate', 'disable'].includes(this.config.encrypt)) throw new TypeError('invalid SQL Server encrypt policy');
    if (this.config.encrypt === 'disable' && !['localhost', '127.0.0.1', '::1'].includes(this.config.host)) throw new TypeError('SQL Server encryption may be disabled only for loopback');
    if (this.config.statementTimeoutMs < 1 || this.config.statementTimeoutMs > 2147483647 || this.config.connectTimeoutMs <= 0 || this.config.maxRows <= 0 || this.config.maxResponseBytes <= 0) throw new TypeError('invalid SQL Server bounds');
  }
  /** Bind the read operation. */ query(): QueryRowsOperation { return new QueryRowsOperation(this); }
  /** Bind the write operation. */ execute(): ExecuteStatementOperation { return new ExecuteStatementOperation(this); }
  /** Build a receipt without including connection settings or credentials. */
  receipt(call: Call): ReceiptDraft { return { provider: 'microsoft-sql-server', callId: call.id, observedAt: this.now().toISOString(), ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}) }; }
  /** Resolve the password for this call and open a fresh native driver connection. */
  async open(call: Call): Promise<sql.ConnectionPool> {
    const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
    if (!(credentials.password instanceof SecretString) || !credentials.password.reveal()) throw new TypeError('SQL Server connection credentials are unavailable');
    const configuration: sql.config = {
      server: this.config.host, port: this.config.port, database: this.config.database, user: this.config.user, password: credentials.password.reveal(),
      connectionTimeout: this.config.connectTimeoutMs, requestTimeout: this.config.statementTimeoutMs,
      pool: { min: 0, max: 1 },
      options: { encrypt: this.config.encrypt === 'strict' ? 'strict' : this.config.encrypt !== 'disable', trustServerCertificate: this.config.encrypt === 'trust-server-certificate', enableArithAbort: true, fallbackToDefaultDb: false },
    };
    const pool = new sql.ConnectionPool(configuration);
    await pool.connect();
    return pool;
  }
  /** Convert a classified error into the connector failure shape. */
  failure(operation: string, classification: ClassifiedFailure): Failure { return { provider: 'microsoft-sql-server', operation, kind: classification.kind, message: classification.message }; }
}
