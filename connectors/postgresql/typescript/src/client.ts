// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { isIP } from 'node:net';
import pg, { type Client as PgClient } from 'pg';
import type { Call, CredentialProvider, ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { ExecuteStatementOperation, QueryRowsOperation } from './operations.js';
import type { ClassifiedFailure } from './failures.js';

/** Injectable native driver factory for deterministic connector tests. */
export interface PostgreSQLClientOptions {
  /** Build one short-lived PostgreSQL connection for each Step attempt. */
  createDriver?: (configuration: pg.ClientConfig) => PgClient;
  /** Clock for secret-free provider receipts. */
  now?: () => Date;
}

/** One validated PostgreSQL connection configuration and rotating credential source. */
export class PostgreSQLClient {
  readonly configuration: Required<Config>;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly createDriver: (configuration: pg.ClientConfig) => PgClient;
  private readonly now: () => Date;

  /** Validate connection fields without contacting PostgreSQL. */
  constructor(configuration: Config, credentials: CredentialProvider<Credentials>, options: PostgreSQLClientOptions = {}) {
    if (!credentials) throw new TypeError('postgresql credential provider is required');
    if (!configuration) throw new TypeError('postgresql connection configuration is required');
    const selected = {
      ...configuration,
      port: configuration.port || 5432,
      sslMode: configuration.sslMode || 'require',
      connectTimeoutMs: configuration.connectTimeoutMs || 5000,
      statementTimeoutMs: configuration.statementTimeoutMs || 5000,
      maxRows: configuration.maxRows || 1000,
      maxResponseBytes: configuration.maxResponseBytes || 1048576,
    };
    if (!selected.host || selected.host.trim() !== selected.host || /[,='\\\s]/u.test(selected.host) || hasControlCharacter(selected.host) || selected.host.includes('://')) throw new TypeError('postgresql host must name one server without whitespace or connection syntax');
    if (!Number.isInteger(selected.port) || selected.port < 1 || selected.port > 65535) throw new TypeError('postgresql port must be from 1 through 65535');
    for (const [name, value] of [['database', selected.database], ['user', selected.user]] as const) {
      if (!value?.trim() || hasControlCharacter(value)) throw new TypeError(`postgresql ${name} must be non-blank and contain no control characters`);
    }
    if (!['require', 'verify-ca', 'verify-full', 'disable'].includes(selected.sslMode)) throw new TypeError('postgresql sslMode is invalid');
    const isLocal = selected.host === 'localhost' || selected.host.startsWith('/') || (isIP(selected.host) !== 0 && (selected.host === '::1' || selected.host.startsWith('127.')));
    if (selected.sslMode === 'disable' && !isLocal) throw new TypeError('postgresql sslMode disable requires a loopback address or Unix socket');
    if (!Number.isSafeInteger(selected.connectTimeoutMs) || selected.connectTimeoutMs < 1 || !Number.isSafeInteger(selected.statementTimeoutMs) || selected.statementTimeoutMs < 1) throw new TypeError('postgresql timeouts must be positive milliseconds');
    if (!Number.isSafeInteger(selected.maxRows) || selected.maxRows < 1 || selected.maxRows > 100000) throw new TypeError('postgresql maxRows must be from 1 through 100000');
    if (!Number.isSafeInteger(selected.maxResponseBytes) || selected.maxResponseBytes < 1 || selected.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('postgresql maxResponseBytes must be from 1 through 16777216');
    this.configuration = selected;
    this.credentials = credentials;
    this.createDriver = options.createDriver ?? (settings => new pg.Client(settings));
    this.now = options.now ?? (() => new Date());
    if (typeof this.now !== 'function') throw new TypeError('postgresql connector clock is required');
  }

  /** Bind the read-only query operation. */
  query(): QueryRowsOperation { return new QueryRowsOperation(this); }

  /** Bind the transactional write operation. */
  execute(): ExecuteStatementOperation { return new ExecuteStatementOperation(this); }

  /** Resolve the current password outside Flow state and open one native connection. */
  async connect(call: Call): Promise<PgClient | null> {
    let password: string;
    try { password = (await this.credentials.resolve(call)).password.reveal(); }
    catch { return null; }
    if (!password) return null;
    const settings = this.configuration;
    const rootCertificate = process.env.PGSSLROOTCERT;
    const ssl = settings.sslMode === 'disable' || settings.host.startsWith('/') ? false : {
      rejectUnauthorized: settings.sslMode !== 'require',
      ...(settings.sslMode === 'verify-ca' ? { checkServerIdentity: () => undefined } : {}),
      ...(rootCertificate ? { ca: readFileSync(rootCertificate, 'utf8') } : {}),
    };
    const connection = this.createDriver({ host: settings.host, port: settings.port, database: settings.database, user: settings.user, password, ssl, connectionTimeoutMillis: settings.connectTimeoutMs, application_name: 'dex-postgresql-connector', options: '-c client_encoding=UTF8' });
    await connection.connect();
    return connection;
  }

  /** Attach only stable, secret-free PostgreSQL correlation fields. */
  receipt(call: Call, connection?: PgClient, failure?: ClassifiedFailure): ReceiptDraft {
    const metadata: Record<string, string> = {};
    const backendPid = (connection as (PgClient & { processID?: number }) | undefined)?.processID;
    if (backendPid) metadata.backendPid = String(backendPid);
    if (failure?.sqlState) metadata.sqlState = failure.sqlState;
    return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'postgresql', observedAt: this.now().toISOString(), ...(Object.keys(metadata).length ? { metadata } : {}) };
  }
}

function hasControlCharacter(value: string): boolean { return [...value].some(character => { const code = character.codePointAt(0) ?? 0; return code < 32 || code === 127; }); }
