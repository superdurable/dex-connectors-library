// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetPackageDefinition } from './generated/definitions.js';
import type { Config, Credentials, PackageStatus, GetPackageInput } from './generated/models.js';

const defaultBaseURL = 'https://sandbox.esignlive.com/api';
const emptyPackage = (): PackageStatus => ({ id: '', name: '', status: '' });

/** Injectable provider sender for tests and local applications. */
export interface OneSpanSignClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated OneSpan Sign REST API client for package metadata. */
export class OneSpanSignClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting OneSpanSign. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: OneSpanSignClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('OneSpan Sign configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('OneSpan Sign request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getPackage Query. */ getPackage(): GetPackageOperation { return new GetPackageOperation(this); }

  /** Resolve the API key and send one bounded package lookup. */
  async get(call: Call, packageID: string): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiKey || !isHeaderSafeCredential(apiKey)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/packages/${encodeURIComponent(packageID)}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${apiKey}` },
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

/** Read one package's ID, name, and status. */
export class GetPackageOperation implements Query<GetPackageInput, PackageStatus> {
  /** Bind the provider client. */ constructor(private readonly client: OneSpanSignClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetPackageDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetPackageInput): Promise<QueryAttempt<PackageStatus>> {
    if (!isValidPackageID(input?.packageId))
      return terminal('defect', 'VALIDATION', 'package ID must be 1–256 printable characters');
    const exchange = await this.client.get(call, input.packageId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'OneSpan Sign credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'OneSpan Sign request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'OneSpan Sign rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'OneSpan Sign is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'OneSpan Sign did not expose the package');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `OneSpan Sign rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'OneSpan Sign response exceeded the configured limit');
    const packageStatus = decodePackage(exchange.body, input.packageId);
    if (!packageStatus) return terminal('invalidResponse', 'PROTOCOL', 'OneSpan Sign returned invalid package JSON');
    return newQueryBranch('found', packageStatus);
  }
}

function isValidPackageID(value: unknown): value is string {
  if (typeof value !== 'string' || !value || Buffer.byteLength(value, 'utf8') > 256) return false;
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint < 32 || (codePoint >= 127 && codePoint <= 159) || '/?#'.includes(character)) return false;
  }
  return true;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'onespan-sign', operation: 'getPackage', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<PackageStatus> { return newQueryBranch(branch, emptyPackage(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<PackageStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodePackage(body: Uint8Array, packageID: string): PackageStatus | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || raw.id !== packageID || typeof raw.name !== 'string' || !raw.name.trim() || typeof raw.status !== 'string' || !raw.status.trim()) return undefined;
  return { id: packageID, name: raw.name, status: raw.status };
}
