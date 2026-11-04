// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createPrivateKey, createPublicKey, sign } from 'node:crypto';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { SubmitStatementOperation, GetStatementResultOperation, CancelStatementOperation } from './operations.js';

const handlePattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const codePattern = /^[0-9]{6}$/;
const sqlStatePattern = /^[0-9A-Z]{5}$/;
const maxBodyBytes = 256 << 20;

/** A request sender injectable for provider replay. */
export type SnowflakeRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport and deterministic clock. */
export interface SnowflakeClientOptions {
  /** Use only a loopback URL for local provider verification. */
  localProviderUrl?: string;
  /** Override the bounded SDK HTTP transport. */
  send?: SnowflakeRequestSender;
  /** Receipt and JWT clock. */
  now?: () => Date;
}
/** Secret-safe outcome for a failed exchange. */
export interface SnowflakeFailure {
  disposition: 'retry' | 'rejected' | 'invalidResponse' | 'defect';
  failure: Failure;
  retryAfterMs: number;
  status: SnowflakeStatus;
}
/** Validated fields from a SQL API status body. */
export interface SnowflakeStatus {
  code?: string;
  sqlState?: string;
  statementHandle?: string;
  createdOn?: number;
}
/** One response or classified failure. */
export type SnowflakeExchange = { response: ProviderResponse } | { outcome: SnowflakeFailure };

/** Immutable Snowflake SQL API client that resolves credentials for each call. */
export class SnowflakeClient {
  readonly statementTimeoutSeconds: number;
  readonly maxRows: number;
  readonly maxResponseBytes: number;
  readonly context: { warehouse: string | undefined; role: string | undefined; database: string | undefined; schema: string | undefined };
  private readonly baseUrl: string;
  private readonly jwtAccount: string;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: SnowflakeRequestSender;
  private readonly now: () => Date;

  /** Validate connection settings without contacting Snowflake. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: SnowflakeClientOptions = {}) {
    if (!credentials) throw new TypeError('snowflake credential source is required');
    const identifier = config.accountIdentifier;
    if (!identifier || identifier.trim() !== identifier || /[:/@?#]/.test(identifier) || identifier.toLowerCase().includes('snowflakecomputing.com')) throw new TypeError('snowflake accountIdentifier is invalid');
    const account = identifier.toLowerCase().replace(/\.privatelink$/, '');
    const first = account.split('.')[0] ?? '';
    if (!(/^[a-z][a-z0-9]*-[a-z][a-z0-9_]*$/.test(account) || (!first.includes('-') && /^[a-z][a-z0-9]*(\.[a-z0-9]+(-[a-z0-9]+)*){0,2}$/.test(account)))) throw new TypeError('snowflake accountIdentifier is invalid');
    const host = `${identifier.toLowerCase()}.snowflakecomputing.com`;
    if (host.length > 253) throw new TypeError('snowflake accountIdentifier is too long');
    this.jwtAccount = first.toUpperCase();
    this.baseUrl = options.localProviderUrl ? validateLocalProviderUrl(options.localProviderUrl) : `https://${host}`;
    this.statementTimeoutSeconds = config.statementTimeoutSeconds || 3600;
    this.maxRows = config.maxRows || 1000;
    this.maxResponseBytes = config.maxResponseBytes || 1048576;
    if (!Number.isSafeInteger(this.statementTimeoutSeconds) || this.statementTimeoutSeconds < 1 || this.statementTimeoutSeconds > 604800) throw new TypeError('snowflake statementTimeoutSeconds must be from 1 through 604800');
    if (!Number.isSafeInteger(this.maxRows) || this.maxRows < 1 || this.maxRows > 100000) throw new TypeError('snowflake maxRows must be from 1 through 100000');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1 || this.maxResponseBytes > 16777216) throw new TypeError('snowflake maxResponseBytes must be from 1 through 16777216');
    for (const [field, value] of Object.entries({ warehouse: config.warehouse, role: config.role, database: config.database, schema: config.schema })) {
      if (value && (value.trim() !== value || Buffer.byteLength(value) > 255 || /["\p{Cc}]/u.test(value))) throw new TypeError(`snowflake ${field} is invalid`);
    }
    this.context = { warehouse: config.warehouse, role: config.role, database: config.database, schema: config.schema };
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the asynchronous statement submission. */ submitStatement() { return new SubmitStatementOperation(this); }
  /** Bind the status and result read. */ getStatementResult() { return new GetStatementResultOperation(this); }
  /** Bind statement cancellation. */ cancelStatement() { return new CancelStatementOperation(this); }

  /** Create a secret-safe correlation receipt. */
  receipt(call: Call, handle = '', status: SnowflakeStatus = {}): ReceiptDraft {
    const metadata: Record<string, string> = {};
    if (status.code && codePattern.test(status.code)) metadata.code = status.code;
    if (status.sqlState && sqlStatePattern.test(status.sqlState)) metadata.sqlState = status.sqlState;
    return { callId: call.id, provider: 'snowflake', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(handlePattern.test(handle) ? { providerObjectId: handle } : {}),
      ...(Object.keys(metadata).length ? { metadata } : {}) };
  }

  /** Resolve a credential and send one SQL API request. */
  async exchange(call: Call, operation: string, method: string, target: string, body?: Uint8Array): Promise<SnowflakeExchange> {
    let token: string;
    let tokenType: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (credentials.authMethodId === 'programmatic-access-token') {
        if (!(credentials.programmaticAccessToken instanceof SecretString) || !isHeaderSafeCredential(credentials.programmaticAccessToken.reveal())) throw new Error('the Snowflake programmatic access token is blank or contains spaces or non-ASCII characters');
        token = credentials.programmaticAccessToken.reveal(); tokenType = 'PROGRAMMATIC_ACCESS_TOKEN';
      } else if (credentials.authMethodId === 'key-pair') {
        token = this.signJwt(credentials); tokenType = 'KEYPAIR_JWT';
      } else throw new Error('the Snowflake connection selects no supported authentication method');
    } catch (error) {
      const message = error instanceof Error && error.message.startsWith('the Snowflake') ? error.message : 'Snowflake connection credentials are unavailable';
      return { outcome: this.failure('defect', operation, 'AUTHENTICATION', message) };
    }
    try {
      const response = await this.send({ url: this.baseUrl + target, method,
        headers: { Authorization: `Bearer ${token}`, 'X-Snowflake-Authorization-Token-Type': tokenType, Accept: 'application/json', 'User-Agent': 'dex-snowflake-connector/1.0', ...(body ? { 'Content-Type': 'application/json' } : {}) },
        ...(body ? { body } : {}), maxResponseBytes: maxBodyBytes,
        addressPolicy: this.baseUrl.startsWith('https://') && !this.baseUrl.includes('localhost') && !this.baseUrl.includes('127.0.0.1') ? 'publicHTTPS' : 'privateNetworkHTTP',
        timeoutMs: 20000, signal: call.context.cancellationSignal });
      if (response.status === 200 || response.status === 202) return { response };
      return { outcome: this.classifyResponse(response, operation) };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.status !== 200 && error.status !== 202) {
        return { outcome: this.classifyResponse({ status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, operation) };
      }
      if (error instanceof BodyTooLargeError && operation === 'getStatementResult') {
        return { outcome: this.failure('invalidResponse', operation, 'RESPONSE_TOO_LARGE', `the Snowflake response exceeds ${maxBodyBytes} bytes`) };
      }
      return { outcome: this.failure('retry', operation, 'TRANSPORT', 'the Snowflake SQL API request failed or its response could not be read') };
    }
  }

  /** Build one failure with no provider-controlled message. */
  failure(disposition: SnowflakeFailure['disposition'], operation: string, kind: Failure['kind'], message: string, status: SnowflakeStatus = {}, retryAfterMs = 0): SnowflakeFailure {
    return { disposition, failure: { kind, provider: 'snowflake', operation, message }, retryAfterMs, status };
  }

  private signJwt(credentials: Credentials): string {
    const user = credentials.user;
    if (!user || user.trim() !== user || Buffer.byteLength(user) > 255 || /["\p{Cc}]/u.test(user)) throw new Error('the Snowflake key-pair user name is blank, too long, or contains quotes or control characters');
    if (!(credentials.privateKey instanceof SecretString)) throw new Error('the Snowflake private key is not an unencrypted PEM RSA private key');
    let key;
    try {
      const pem = credentials.privateKey.reveal();
      if (pem.includes('BEGIN ENCRYPTED PRIVATE KEY')) throw new Error('the Snowflake private key is encrypted; export an unencrypted PKCS #8 key with openssl pkcs8 -topk8 -nocrypt');
      key = createPrivateKey(pem);
      if (key.asymmetricKeyType !== 'rsa') throw new Error('non-RSA');
    } catch (error) {
      if (error instanceof Error && error.message.startsWith('the Snowflake private key is encrypted')) throw error;
      throw new Error('the Snowflake private key is not an unencrypted PEM RSA private key');
    }
    const publicDer = createPublicKey(key).export({ type: 'spki', format: 'der' });
    const fingerprint = `SHA256:${createHash('sha256').update(publicDer).digest('base64')}`;
    const subject = `${this.jwtAccount}.${user.toUpperCase()}`;
    const issuedAt = Math.floor(this.now().getTime() / 1000);
    const header = Buffer.from('{"alg":"RS256","typ":"JWT"}').toString('base64url');
    const claims = Buffer.from(JSON.stringify({ iss: `${subject}.${fingerprint}`, sub: subject, iat: issuedAt, exp: issuedAt + 3540 })).toString('base64url');
    const unsigned = `${header}.${claims}`;
    return `${unsigned}.${sign('RSA-SHA256', Buffer.from(unsigned), key).toString('base64url')}`;
  }

  private classifyResponse(response: ProviderResponse, operation: string): SnowflakeFailure {
    const status = decodeStatus(response.body);
    const code = response.status;
    let kind: Failure['kind'] = 'PROVIDER_REJECTION';
    let disposition: SnowflakeFailure['disposition'] = 'rejected';
    if (code === 429) { kind = 'RATE_LIMIT'; disposition = 'retry'; }
    else if ([500, 502, 503, 504].includes(code)) { kind = 'AVAILABILITY'; disposition = 'retry'; }
    else if (code === 401) kind = 'AUTHENTICATION';
    else if (code === 403) kind = 'AUTHORIZATION';
    else if (code === 404) kind = 'NOT_FOUND';
    else if (code === 400) kind = 'VALIDATION';
    else if (code === 408) kind = 'AVAILABILITY';
    else if (code === 422) kind = statementFailureKind(status);
    else if (code >= 300 && code < 400) kind = 'PROTOCOL';
    let message = `Snowflake returned HTTP ${code}`;
    if (status.code) message += ` with code ${status.code}`;
    if (status.sqlState) message += ` and SQLSTATE ${status.sqlState}`;
    if (code === 408 || status.code === '000604') message += '; the statement exceeded statementTimeoutSeconds or was canceled';
    else if (code >= 300 && code < 400) message += '; the connector never follows redirects, so check accountIdentifier';
    const retry = response.headers['retry-after'];
    return this.failure(disposition, operation, kind, message, status, disposition === 'retry' ? parseRetryAfter(Array.isArray(retry) ? retry[0] ?? '' : retry ?? '', this.now()) : 0);
  }
}

/** Decode only safe status tokens. */
export function decodeStatus(body: Uint8Array): SnowflakeStatus {
  try {
    const value: unknown = JSON.parse(Buffer.from(body.subarray(0, 65536)).toString('utf8'));
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
    const status = value as Record<string, unknown>;
    return { ...(typeof status.code === 'string' && codePattern.test(status.code) ? { code: status.code } : {}),
      ...(typeof status.sqlState === 'string' && sqlStatePattern.test(status.sqlState) ? { sqlState: status.sqlState } : {}),
      ...(typeof status.statementHandle === 'string' && handlePattern.test(status.statementHandle) ? { statementHandle: status.statementHandle } : {}),
      ...(typeof status.createdOn === 'number' && Number.isSafeInteger(status.createdOn) ? { createdOn: status.createdOn } : {}) };
  } catch { return {}; }
}

/** Check a Snowflake statement handle before placing it in a path. */
export function isStatementHandle(value: string): boolean { return handlePattern.test(value); }

function statementFailureKind(status: SnowflakeStatus): Failure['kind'] {
  if (status.code === '000709' || status.sqlState === '02000') return 'NOT_FOUND';
  if (status.code === '000604' || status.sqlState === '57014') return 'AVAILABILITY';
  if (status.sqlState?.startsWith('28')) return 'AUTHENTICATION';
  if (status.sqlState === '42501') return 'AUTHORIZATION';
  if (status.sqlState?.startsWith('23')) return 'CONFLICT';
  if (status.sqlState && ['22', '42', '07'].some(prefix => status.sqlState!.startsWith(prefix))) return 'VALIDATION';
  return 'PROVIDER_REJECTION';
}

function validateLocalProviderUrl(value: string): string {
  const base = validateBaseURL(value);
  const parsed = new URL(base);
  if (parsed.pathname !== '/' || !['localhost', '127.0.0.1', '[::1]'].includes(parsed.hostname)) throw new TypeError('snowflake local provider URL must use a loopback host without a path');
  return base;
}
