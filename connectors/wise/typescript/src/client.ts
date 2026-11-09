// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTransferDefinition } from './generated/definitions.js';
import type { Config, Credentials, TransferStatus, GetTransferInput } from './generated/models.js';

const defaultBaseURL = 'https://api.wise.com/2026Q4';
const emptyTransfer = (): TransferStatus => ({ id: '', status: '', sourceCurrency: '' });

/** Injectable provider sender for tests and local applications. */
export interface WiseClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Wise REST API client for transfer metadata. */
export class WiseClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Wise. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: WiseClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Wise configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Wise request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getTransfer Query. */ getTransfer(): GetTransferOperation { return new GetTransferOperation(this); }

  /** Resolve the API token and send one bounded transfer lookup. */
  async get(call: Call, transferID: string): Promise<Exchange> {
    let apiToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiToken = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiToken || !isHeaderSafeCredential(apiToken)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/transfers/${encodeURIComponent(transferID)}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${apiToken}` },
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

/** Read one transfer's ID, status, and source currency. */
export class GetTransferOperation implements Query<GetTransferInput, TransferStatus> {
  /** Bind the provider client. */ constructor(private readonly client: WiseClient) {}
  /** Return the generated Query transfer. */ definition(): QueryDefinition { return GetTransferDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetTransferInput): Promise<QueryAttempt<TransferStatus>> {
    if (!isValidTransferID(input?.transferId))
      return terminal('defect', 'VALIDATION', 'transfer ID must be a positive decimal integer');
    const exchange = await this.client.get(call, input.transferId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Wise credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Wise request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Wise rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Wise is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Wise did not expose the transfer');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Wise rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Wise response exceeded the configured limit');
    const transferStatus = decodeTransfer(exchange.body, input.transferId);
    if (!transferStatus) return terminal('invalidResponse', 'PROTOCOL', 'Wise returned invalid transfer JSON');
    return newQueryBranch('found', transferStatus);
  }
}

function isValidTransferID(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9][0-9]{0,18}$/.test(value) && BigInt(value) <= 9007199254740991n;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'wise', operation: 'getTransfer', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<TransferStatus> { return newQueryBranch(branch, emptyTransfer(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<TransferStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeTransfer(body: Uint8Array, transferID: string): TransferStatus | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw)) return undefined;
  if (typeof raw.id !== 'number' || !Number.isSafeInteger(raw.id) || String(raw.id) !== transferID ||
    typeof raw.status !== 'string' || !raw.status.trim() ||
    typeof raw.sourceCurrency !== 'string' || raw.sourceCurrency.length !== 3) return undefined;
  return { id: transferID, status: raw.status, sourceCurrency: raw.sourceCurrency };
}
