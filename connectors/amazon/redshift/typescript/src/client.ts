// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetStatementResultDefinition } from './generated/definitions.js';
import type { Config, Credentials, StatementResultPage, GetStatementResultInput } from './generated/models.js';

const emptyPage = (): StatementResultPage => ({ records: [], nextToken: '', totalNumRows: 0 });
const regionPattern = /^[a-z]{2}(?:-gov)?-[a-z]+-[0-9]+$/;
const statementIDPattern = /^[a-z0-9]{8}(?:-[a-z0-9]{4}){3}-[a-z0-9]{12}(?::[0-9]{1,2})?$/;
/** Injectable transport and clock for tests and local applications. */
export interface AmazonRedshiftClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Signing clock; omitted uses the current time. */ now?: () => Date;
}
interface Exchange { status: number; body: Uint8Array; retryAfterMs: number; error?: 'credential' | 'request' | 'tooLarge' }

/** Authenticated Redshift client for signed GetStatementResult requests. */
export class AmazonRedshiftClient {
  private readonly endpoint: string;
  private readonly region: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate the Region, endpoint, and limits without contacting AWS. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: AmazonRedshiftClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Redshift configuration and credentials are required');
    this.region = config.region || 'us-east-1';
    if (!regionPattern.test(this.region)) throw new TypeError('Redshift Region is invalid');
    this.endpoint = validateBaseURL(config.endpoint || `https://redshift-data.${this.region}.amazonaws.com`).replace(/\/+$/, '');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Redshift request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the getStatementResult Query. */ getStatementResult(): GetStatementResultOperation { return new GetStatementResultOperation(this); }

  /** Sign and send one bounded GetStatementResult request. */
  async readStatementResult(call: Call, body: Buffer): Promise<Exchange> {
    let accessKey: string; let secretKey: string; let sessionToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessKey = credentials.accessKeyId instanceof SecretString ? credentials.accessKeyId.reveal() : '';
      secretKey = credentials.secretAccessKey instanceof SecretString ? credentials.secretAccessKey.reveal() : '';
      sessionToken = credentials.sessionToken instanceof SecretString ? credentials.sessionToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!accessKey || !secretKey || !isHeaderSafeCredential(accessKey) || !isHeaderSafeCredential(secretKey) ||
      (sessionToken && !isHeaderSafeCredential(sessionToken))) return emptyExchange('credential');
    const url = `${this.endpoint}/`;
    const timestamp = this.now().toISOString().replace(/[-:]|\.\d{3}/g, '');
    const day = timestamp.slice(0, 8);
    const headersForSignature: Record<string, string> = {
      'content-length': String(body.length), 'content-type': 'application/x-amz-json-1.1', host: new URL(url).host,
      'x-amz-date': timestamp, 'x-amz-target': 'RedshiftData.GetStatementResult',
    };
    if (sessionToken) headersForSignature['x-amz-security-token'] = sessionToken;
    const signedHeaders = Object.keys(headersForSignature).sort().join(';');
    const canonicalHeaders = Object.entries(headersForSignature).sort(([left], [right]) => left.localeCompare(right)).map(([name, value]) => `${name}:${value.trim()}\n`).join('');
    const payloadHash = createHash('sha256').update(body).digest('hex');
    const canonicalRequest = `POST\n/\n\n${canonicalHeaders}\n${signedHeaders}\n${payloadHash}`;
    const scope = `${day}/${this.region}/redshift-data/aws4_request`;
    const stringToSign = `AWS4-HMAC-SHA256\n${timestamp}\n${scope}\n${createHash('sha256').update(canonicalRequest).digest('hex')}`;
    const hmac = (key: Buffer | string, value: string) => createHmac('sha256', key).update(value).digest();
    const signingKey = hmac(hmac(hmac(hmac(`AWS4${secretKey}`, day), this.region), 'redshift-data'), 'aws4_request');
    const signature = createHmac('sha256', signingKey).update(stringToSign).digest('hex');
    const headers: Record<string, string> = {
      'Content-Type': headersForSignature['content-type']!, 'X-Amz-Date': timestamp,
      'X-Amz-Target': headersForSignature['x-amz-target']!,
      Authorization: `AWS4-HMAC-SHA256 Credential=${accessKey}/${scope}, SignedHeaders=${signedHeaders}, Signature=${signature}`,
    };
    if (sessionToken) headers['X-Amz-Security-Token'] = sessionToken;
    const request: ProviderRequest = { url, method: 'POST', headers, body,
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.endpoint.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isTooLarge = true;
      } else return emptyExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (isTooLarge || response.body.length > this.maxResponseBytes)
      return { status: response.status, body: new Uint8Array(), retryAfterMs, error: 'tooLarge' };
    if ([accessKey, secretKey, sessionToken].some(secret => secret && Buffer.from(response.body).includes(secret)))
      return { ...emptyExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Fetch one cached SQL result page by statement UUID. */
export class GetStatementResultOperation implements Query<GetStatementResultInput, StatementResultPage> {
  /** Bind the provider client. */ constructor(private readonly client: AmazonRedshiftClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetStatementResultDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetStatementResultInput): Promise<QueryAttempt<StatementResultPage>> {
    if (!input || typeof input.statementId !== 'string' || !statementIDPattern.test(input.statementId) ||
      typeof input.nextToken !== 'string' || input.nextToken.length > 4096 ||
      [...input.nextToken].some(character => character.charCodeAt(0) === 0 || character === '\r' || character === '\n'))
      return terminal('defect', 'VALIDATION', 'Redshift statement ID or page token is invalid');
    const body = Buffer.from(JSON.stringify({ Id: input.statementId, ...(input.nextToken ? { NextToken: input.nextToken } : {}) }));
    const exchange = await this.client.readStatementResult(call, body);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Redshift credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Redshift request failed');
    if (exchange.status === 429 || (exchange.status !== 200 && isThrottled(exchange.body))) return retry('RATE_LIMIT', 'Redshift rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Redshift is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404 || (exchange.status === 400 && hasErrorCode(exchange.body, 'ResourceNotFoundException')))
      return newQueryBranch('missing', emptyPage());
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Redshift rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Redshift response exceeded the configured limit');
    let document: unknown;
    try { document = JSON.parse(Buffer.from(exchange.body).toString('utf8')) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Redshift returned invalid result JSON'); }
    if (!isObject(document) || !Array.isArray(document.Records) ||
      (document.NextToken !== undefined && typeof document.NextToken !== 'string') ||
      (document.TotalNumRows !== undefined && (typeof document.TotalNumRows !== 'number' || !Number.isSafeInteger(document.TotalNumRows) || document.TotalNumRows < 0)) ||
      document.Records.some(row => !Array.isArray(row) || row.some((field: unknown) => !isObject(field) || Object.keys(field).length !== 1)))
      return terminal('invalidResponse', 'PROTOCOL', 'Redshift returned invalid result JSON');
    return newQueryBranch('fetched', { records: document.Records, nextToken: document.NextToken ?? '', totalNumRows: document.TotalNumRows ?? 0 });
  }
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function hasErrorCode(body: Uint8Array, expected: string): boolean {
  try { const document = JSON.parse(Buffer.from(body).toString('utf8')) as unknown;
    return isObject(document) && typeof document.__type === 'string' && document.__type.split('#').at(-1) === expected;
  } catch { return false; }
}
function isThrottled(body: Uint8Array): boolean {
  return ['ThrottlingException', 'ActiveStatementsExceededException', 'ActiveSessionsExceededException'].some(code => hasErrorCode(body, code));
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'amazon-redshift', operation: 'getStatementResult', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<StatementResultPage> { return newQueryBranch(branch, emptyPage(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<StatementResultPage> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
