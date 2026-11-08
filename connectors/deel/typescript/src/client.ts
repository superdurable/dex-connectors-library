// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetContractDefinition } from './generated/definitions.js';
import type { Config, Credentials, ContractStatus, GetContractInput } from './generated/models.js';

const defaultBaseURL = 'https://api-staging.letsdeel.com/rest';
const emptyContract = (): ContractStatus => ({ id: '', status: '', type: '' });

/** Injectable provider sender for tests and local applications. */
export interface DeelClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Deel REST API client for contract metadata. */
export class DeelClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Deel. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: DeelClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Deel configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Deel request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getContract Query. */ getContract(): GetContractOperation { return new GetContractOperation(this); }

  /** Resolve the API token and send one bounded contract lookup. */
  async get(call: Call, contractID: string): Promise<Exchange> {
    let apiToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiToken = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiToken || !isHeaderSafeCredential(apiToken)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/contracts/${encodeURIComponent(contractID)}`, method: 'GET',
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

/** Read one contract's ID, type, and status. */
export class GetContractOperation implements Query<GetContractInput, ContractStatus> {
  /** Bind the provider client. */ constructor(private readonly client: DeelClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetContractDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetContractInput): Promise<QueryAttempt<ContractStatus>> {
    if (!isValidContractID(input?.contractId))
      return terminal('defect', 'VALIDATION', 'contract ID must be 1–128 URL-safe characters');
    const exchange = await this.client.get(call, input.contractId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Deel credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Deel request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Deel rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Deel is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Deel did not expose the contract');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Deel rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Deel response exceeded the configured limit');
    const contractStatus = decodeContract(exchange.body, input.contractId);
    if (!contractStatus) return terminal('invalidResponse', 'PROTOCOL', 'Deel returned invalid contract JSON');
    return newQueryBranch('found', contractStatus);
  }
}

function isValidContractID(value: unknown): value is string {
  return typeof value === 'string' && /^[a-zA-Z0-9_-]{1,128}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'deel', operation: 'getContract', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ContractStatus> { return newQueryBranch(branch, emptyContract(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<ContractStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeContract(body: Uint8Array, contractID: string): ContractStatus | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !isObject(raw.data)) return undefined;
  const data = raw.data;
  if (data.id !== contractID || typeof data.status !== 'string' || !data.status.trim() ||
    typeof data.type !== 'string' || !data.type.trim()) return undefined;
  return { id: contractID, status: data.status, type: data.type };
}
