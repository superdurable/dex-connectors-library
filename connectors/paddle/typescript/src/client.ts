// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTransactionDefinition } from './generated/definitions.js';
import type { Config, Credentials, TransactionStatus, GetTransactionInput } from './generated/models.js';

const defaultBaseURL = 'https://sandbox-api.paddle.com';
const emptyTransaction = (): TransactionStatus => ({ id: '', status: '', currencyCode: '' });

/** Injectable provider sender for tests and local applications. */
export interface PaddleClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Paddle REST API client for transaction metadata. */
export class PaddleClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Paddle. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: PaddleClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Paddle configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Paddle request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getTransaction Query. */ getTransaction(): GetTransactionOperation { return new GetTransactionOperation(this); }

  /** Resolve the API key and send one bounded transaction lookup. */
  async get(call: Call, transactionID: string): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiKey || !isHeaderSafeCredential(apiKey)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/transactions/${encodeURIComponent(transactionID)}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
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
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read one transaction's ID, name, and status. */
export class GetTransactionOperation implements Query<GetTransactionInput, TransactionStatus> {
  /** Bind the provider client. */ constructor(private readonly client: PaddleClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetTransactionDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetTransactionInput): Promise<QueryAttempt<TransactionStatus>> {
    if (!isValidTransactionID(input?.transactionId))
      return terminal('defect', 'VALIDATION', 'transaction ID must start with txn_ and contain 26 lowercase letters or digits');
    const exchange = await this.client.get(call, input.transactionId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Paddle credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Paddle request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Paddle rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Paddle is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Paddle did not expose the transaction');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Paddle rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Paddle response exceeded the configured limit');
    const transactionStatus = decodeTransaction(exchange.body, input.transactionId);
    if (!transactionStatus) return terminal('invalidResponse', 'PROTOCOL', 'Paddle returned invalid transaction JSON');
    return newQueryBranch('found', transactionStatus);
  }
}

function isValidTransactionID(value: unknown): value is string {
  return typeof value === 'string' && /^txn_[a-z0-9]{26}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'paddle', operation: 'getTransaction', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<TransactionStatus> { return newQueryBranch(branch, emptyTransaction(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<TransactionStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeTransaction(body: Uint8Array, transactionID: string): TransactionStatus | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !isObject(raw.data)) return undefined;
  const data = raw.data;
  if (data.id !== transactionID || typeof data.status !== 'string' || !data.status.trim() ||
    typeof data.currency_code !== 'string' || data.currency_code.length !== 3) return undefined;
  return { id: transactionID, status: data.status, currencyCode: data.currency_code };
}
