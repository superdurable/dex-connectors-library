// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetSignatureRequestDefinition } from './generated/definitions.js';
import type { Config, Credentials, SignatureRequestStatus, GetSignatureRequestInput } from './generated/models.js';

const defaultBaseURL = 'https://api.hellosign.com/v3';
const emptyStatus = (): SignatureRequestStatus => ({ id: '', title: '', isComplete: false, isDeclined: false, hasError: false });

/** Injectable provider sender for tests and local applications. */
export interface DropboxSignClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Dropbox Sign API v3 client for signature-request status. */
export class DropboxSignClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Dropbox Sign. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: DropboxSignClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Dropbox Sign configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Dropbox Sign request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getSignatureRequest Query. */ getSignatureRequest(): GetSignatureRequestOperation { return new GetSignatureRequestOperation(this); }

  /** Resolve the API key and send one bounded signature-request lookup. */
  async get(call: Call, signatureRequestID: string): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiKey || !isHeaderSafeCredential(apiKey) || apiKey.includes(':')) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/signature_request/${encodeURIComponent(signatureRequestID)}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${apiKey}:`).toString('base64')}` },
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

/** Read one signature request's title and completion status. */
export class GetSignatureRequestOperation implements Query<GetSignatureRequestInput, SignatureRequestStatus> {
  /** Bind the provider client. */ constructor(private readonly client: DropboxSignClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetSignatureRequestDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetSignatureRequestInput): Promise<QueryAttempt<SignatureRequestStatus>> {
    if (!isValidSignatureRequestID(input?.signatureRequestId))
      return terminal('defect', 'VALIDATION', 'signature request ID must be 1–256 printable characters');
    const exchange = await this.client.get(call, input.signatureRequestId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Dropbox Sign credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Dropbox Sign request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Dropbox Sign rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Dropbox Sign is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Dropbox Sign did not expose the signature request');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Dropbox Sign rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Dropbox Sign response exceeded the configured limit');
    const status = decodeSignatureRequest(exchange.body, input.signatureRequestId);
    if (!status) return terminal('invalidResponse', 'PROTOCOL', 'Dropbox Sign returned invalid signature request JSON');
    return newQueryBranch('found', status);
  }
}

function isValidSignatureRequestID(value: unknown): value is string {
  if (typeof value !== 'string' || !value || Buffer.byteLength(value, 'utf8') > 256) return false;
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint < 32 || (codePoint >= 127 && codePoint <= 159) || '/?#'.includes(character)) return false;
  }
  return true;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'dropbox-sign', operation: 'getSignatureRequest', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<SignatureRequestStatus> { return newQueryBranch(branch, emptyStatus(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<SignatureRequestStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeSignatureRequest(body: Uint8Array, signatureRequestID: string): SignatureRequestStatus | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !isObject(raw.signature_request)) return undefined;
  const request = raw.signature_request;
  if (request.signature_request_id !== signatureRequestID || typeof request.title !== 'string' || !request.title.trim() ||
    typeof request.is_complete !== 'boolean' || typeof request.is_declined !== 'boolean' || typeof request.has_error !== 'boolean') return undefined;
  return { id: signatureRequestID, title: request.title, isComplete: request.is_complete, isDeclined: request.is_declined, hasError: request.has_error };
}
