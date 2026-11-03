// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { isIP } from 'node:net';
import mysql, { type Connection, type ConnectionOptions } from 'mysql2/promise';
import { SecretString, type Call, type CredentialProvider, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { ExecuteStatementOperation, QueryRowsOperation } from './operations.js';

class MySQLIPIdentityError extends Error {
  readonly code = 'MYSQL_IP_IDENTITY_UNSUPPORTED';
  constructor() { super('MySQL sslMode verify-identity requires a DNS host name; mysql2 cannot verify an IP address against the certificate IP SAN'); }
}

/** Error raised while initializing an authenticated session. */
export class MySQLSessionSetupError extends Error {
  /** Preserve the driver error for secret-safe classification. */
  constructor(readonly driverError: unknown) { super('MySQL session setup failed'); }
}

/** Opens one native MySQL connection per operation. */
export type MySQLConnectionFactory = (options: ConnectionOptions) => Promise<Connection>;
/** Optional dependencies used by deterministic tests. */
export interface MySQLClientOptions {
  /** Connection opener; defaults to the native mysql2 driver. */
  connect?: MySQLConnectionFactory;
  /** Clock for secret-safe receipts. */
  now?: () => Date;
}

/** Validated MySQL connection settings and a live credential source. */
export class MySQLClient {
  /** Effective nonsecret configuration. */
  readonly config: Required<Config>;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly connect: MySQLConnectionFactory;
  private readonly now: () => Date;

  /** Validate settings without contacting the database. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: MySQLClientOptions = {}) {
    if (!credentials) throw new TypeError('MySQL credential provider is required');
    const effective: Required<Config> = {
      host: config.host, port: config.port || 3306, database: config.database, user: config.user,
      sslMode: config.sslMode || 'required', connectTimeoutMs: config.connectTimeoutMs || 5000,
      statementTimeoutMs: config.statementTimeoutMs || 5000, maxRows: config.maxRows || 1000,
      maxResponseBytes: config.maxResponseBytes || 1048576,
    };
    if (!effective.host || /[\s,:]/.test(effective.host) && !isIP(effective.host) || effective.host.includes('://')) throw new TypeError('MySQL host is invalid');
    if (!effective.database || !effective.user || [...effective.database + effective.user].some(character => character.charCodeAt(0) < 32)) throw new TypeError('MySQL database or user is invalid');
    if (!Number.isInteger(effective.port) || effective.port < 1 || effective.port > 65535) throw new TypeError('MySQL port must be from 1 through 65535');
    if (!['required', 'verify-ca', 'verify-identity', 'disabled'].includes(effective.sslMode)) throw new TypeError('MySQL sslMode is invalid');
    const isSocket = effective.host.startsWith('/');
    const isLocal = isSocket || ['localhost', '127.0.0.1', '::1'].includes(effective.host);
    if (effective.sslMode === 'disabled' && !isLocal) throw new TypeError('MySQL sslMode disabled is allowed only for localhost, 127.0.0.1, ::1, or a Unix-socket path');
    if (effective.sslMode !== 'disabled' && isSocket) throw new TypeError('MySQL Unix-socket path requires sslMode disabled');
    if (!Number.isSafeInteger(effective.connectTimeoutMs) || effective.connectTimeoutMs <= 0 || !Number.isSafeInteger(effective.statementTimeoutMs) || effective.statementTimeoutMs < 1 || !Number.isSafeInteger(effective.maxRows) || effective.maxRows < 1 || effective.maxRows > 100000 || !Number.isSafeInteger(effective.maxResponseBytes) || effective.maxResponseBytes < 1 || effective.maxResponseBytes > 16777216) throw new TypeError('MySQL connection bounds are invalid');
    this.config = effective;
    this.credentials = credentials;
    this.connect = options.connect ?? mysql.createConnection;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the read-only query operation. */
  query(): QueryRowsOperation { return new QueryRowsOperation(this); }
  /** Bind the transactional write operation. */
  execute(): ExecuteStatementOperation { return new ExecuteStatementOperation(this); }

  /** Resolve the password for one call without retaining it on the client. */
  async resolvePassword(call: Call): Promise<string | undefined> {
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(resolved.password instanceof SecretString)) return undefined;
      return resolved.password.reveal() || undefined;
    } catch { return undefined; }
  }

  /** Open and initialize one MySQL session with pinned server settings. The callback can bind a call deadline before setup queries. */
  async openSession(password: string, onConnected?: (connection: Connection) => void): Promise<{ connection: Connection; connectionId: string; serverVersion: string }> {
    if (this.config.sslMode === 'verify-identity' && isIP(this.config.host)) throw new MySQLIPIdentityError();
    const settings: ConnectionOptions = {
      ...(this.config.host.startsWith('/') ? { socketPath: this.config.host } : { host: this.config.host, port: this.config.port }),
      database: this.config.database, user: this.config.user, password,
      connectTimeout: this.config.connectTimeoutMs, timezone: 'Z', dateStrings: true,
      supportBigNumbers: true, bigNumberStrings: true, decimalNumbers: false, rowsAsArray: true, jsonStrings: true,
      multipleStatements: false, namedPlaceholders: false, enableKeepAlive: false, flags: ['-FOUND_ROWS', '-LOCAL_FILES'],
    };
    if (this.config.sslMode !== 'disabled') {
      const certificatePath = process.env.MYSQL_SSL_CA;
      let ca: Buffer | undefined;
      if (certificatePath) {
        try { ca = readFileSync(certificatePath); }
        catch { throw new TypeError('the file named by MYSQL_SSL_CA is unreadable'); }
      }
      settings.ssl = { minVersion: 'TLSv1.2', rejectUnauthorized: this.config.sslMode !== 'required', ...(ca ? { ca } : {}), ...(this.config.sslMode === 'verify-identity' ? { verifyIdentity: true } : {}) };
    }
    const connection = await this.connect(settings);
    try {
      onConnected?.(connection);
      const [identity] = await connection.query('SELECT CONNECTION_ID(), VERSION()');
      const row = (identity as unknown[][])[0];
      if (!row || row.length !== 2 || row[0] === undefined || typeof row[1] !== 'string') throw new TypeError('the server returned an unreadable CONNECTION_ID() or VERSION()');
      const serverVersion = row[1];
      const lockSeconds = Math.ceil(this.config.statementTimeoutMs / 1000);
      const timeout = serverVersion.toLowerCase().includes('mariadb') ? `max_statement_time = ${(this.config.statementTimeoutMs / 1000).toFixed(6)}` : `max_execution_time = ${Math.ceil(this.config.statementTimeoutMs)}`;
      await connection.query(`SET SESSION time_zone = '+00:00', sql_mode = 'ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION', wait_timeout = 30, innodb_lock_wait_timeout = ${lockSeconds}, lock_wait_timeout = ${lockSeconds}, ${timeout}`);
      return { connection, connectionId: String(row[0]), serverVersion };
    } catch (error) {
      connection.destroy();
      throw new MySQLSessionSetupError(error);
    }
  }

  /** Make a receipt without SQL, parameters, or credentials. */
  receipt(call: Call, connectionId = '', serverVersion = '', errorNumber?: number, sqlState?: string): ReceiptDraft {
    const metadata: Record<string, string> = {};
    if (connectionId) metadata.connectionId = connectionId;
    if (serverVersion && /^[\x20-\x7e]{1,128}$/.test(serverVersion)) metadata.serverVersion = serverVersion;
    if (errorNumber) metadata.errorNumber = String(errorNumber);
    if (sqlState) metadata.sqlState = sqlState;
    return { callId: call.id, provider: 'mysql', observedAt: this.now().toISOString(), ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), ...(Object.keys(metadata).length ? { metadata } : {}) };
  }
}
