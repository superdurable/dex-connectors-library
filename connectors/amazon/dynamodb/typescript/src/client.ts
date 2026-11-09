// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetItemDefinition } from './generated/definitions.js';
import type { Config, Credentials, ItemResult, GetItemInput } from './generated/models.js';

const emptyItem = (): ItemResult => ({ exists: false, item: {} });
const regionPattern = /^[a-z]{2}(?:-gov)?-[a-z]+-[0-9]+$/;
const tablePattern = /^[A-Za-z0-9_.-]{3,255}$/;
const numberPattern = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[Ee][+-]?[0-9]+)?$/;
/** Injectable transport and clock for tests and local applications. */
export interface AmazonDynamoDBClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Signing clock; omitted uses the current time. */ now?: () => Date;
}
interface Exchange { status: number; body: Uint8Array; retryAfterMs: number; error?: 'credential' | 'request' | 'tooLarge' }

/** Authenticated DynamoDB client for signed GetItem requests. */
export class AmazonDynamoDBClient {
  private readonly endpoint: string;
  private readonly region: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate the Region, endpoint, and limits without contacting AWS. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: AmazonDynamoDBClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('DynamoDB configuration and credentials are required');
    this.region = config.region || 'us-east-1';
    if (!regionPattern.test(this.region)) throw new TypeError('DynamoDB Region is invalid');
    this.endpoint = validateBaseURL(config.endpoint || `https://dynamodb.${this.region}.amazonaws.com`).replace(/\/+$/, '');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('DynamoDB request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the getItem Query. */ getItem(): GetItemOperation { return new GetItemOperation(this); }

  /** Sign and send one bounded GetItem request. */
  async readItem(call: Call, body: Buffer): Promise<Exchange> {
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
      'content-length': String(body.length), 'content-type': 'application/x-amz-json-1.0', host: new URL(url).host,
      'x-amz-date': timestamp, 'x-amz-target': 'DynamoDB_20120810.GetItem',
    };
    if (sessionToken) headersForSignature['x-amz-security-token'] = sessionToken;
    const signedHeaders = Object.keys(headersForSignature).sort().join(';');
    const canonicalHeaders = Object.entries(headersForSignature).sort(([left], [right]) => left.localeCompare(right)).map(([name, value]) => `${name}:${value.trim()}\n`).join('');
    const payloadHash = createHash('sha256').update(body).digest('hex');
    const canonicalRequest = `POST\n/\n\n${canonicalHeaders}\n${signedHeaders}\n${payloadHash}`;
    const scope = `${day}/${this.region}/dynamodb/aws4_request`;
    const stringToSign = `AWS4-HMAC-SHA256\n${timestamp}\n${scope}\n${createHash('sha256').update(canonicalRequest).digest('hex')}`;
    const hmac = (key: Buffer | string, value: string) => createHmac('sha256', key).update(value).digest();
    const signingKey = hmac(hmac(hmac(hmac(`AWS4${secretKey}`, day), this.region), 'dynamodb'), 'aws4_request');
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

/** Read one item by its complete primary key. */
export class GetItemOperation implements Query<GetItemInput, ItemResult> {
  /** Bind the provider client. */ constructor(private readonly client: AmazonDynamoDBClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetItemDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetItemInput): Promise<QueryAttempt<ItemResult>> {
    if (!input || typeof input.tableName !== 'string' || !tablePattern.test(input.tableName) || !validKey(input.key))
      return terminal('defect', 'VALIDATION', 'DynamoDB table or key is invalid');
    const sortedKey = Object.fromEntries(Object.entries(input.key).sort(([left], [right]) => left.localeCompare(right)));
    const body = Buffer.from(JSON.stringify({ TableName: input.tableName, Key: sortedKey, ConsistentRead: input.consistentRead }));
    const exchange = await this.client.readItem(call, body);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'DynamoDB credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'DynamoDB request failed');
    if (exchange.status === 429 || (exchange.status !== 200 && isThrottled(exchange.body))) return retry('RATE_LIMIT', 'DynamoDB rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'DynamoDB is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `DynamoDB rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'DynamoDB response exceeded the configured limit');
    let document: unknown;
    try { document = JSON.parse(Buffer.from(exchange.body).toString('utf8')) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'DynamoDB returned invalid item JSON'); }
    if (!isObject(document)) return terminal('invalidResponse', 'PROTOCOL', 'DynamoDB returned invalid item JSON');
    const item = document.Item;
    if (item === undefined || item === null || (isObject(item) && Object.keys(item).length === 0)) return newQueryBranch('missing', emptyItem());
    if (!isObject(item) || Object.entries(item).some(([name, value]) => !name || !isObject(value) || Object.keys(value).length !== 1))
      return terminal('invalidResponse', 'PROTOCOL', 'DynamoDB returned invalid item JSON');
    return newQueryBranch('found', { exists: true, item });
  }
}
function validKey(key: unknown): key is Record<string, Record<string, string>> {
  if (!isObject(key) || ![1, 2].includes(Object.keys(key).length)) return false;
  return Object.entries(key).every(([name, attribute]) => {
    if (!name || name.length > 255 || [...name].some(character => character.charCodeAt(0) === 0 || character === '\r' || character === '\n') || !isObject(attribute) || Object.keys(attribute).length !== 1) return false;
    const [kind, value] = Object.entries(attribute)[0]!;
    if (typeof value !== 'string' || !value) return false;
    if (kind === 'S') return true;
    if (kind === 'N') return numberPattern.test(value);
    if (kind === 'B') return /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value);
    return false;
  });
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isThrottled(body: Uint8Array): boolean {
  try { const document = JSON.parse(Buffer.from(body).toString('utf8')) as unknown;
    if (!isObject(document) || typeof document.__type !== 'string') return false;
    return ['ProvisionedThroughputExceededException', 'ThrottlingException', 'RequestLimitExceeded', 'TransactionConflictException'].includes(document.__type.split('#').at(-1) ?? '');
  } catch { return false; }
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'amazon-dynamodb', operation: 'getItem', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ItemResult> { return newQueryBranch(branch, emptyItem(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<ItemResult> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
