// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCandidateDefinition } from './generated/definitions.js';
import type { Config, Credentials, CandidateProfile, GetCandidateInput } from './generated/models.js';

const defaultBaseURL = 'https://api.ashbyhq.com';
const emptyCandidate = (): CandidateProfile => ({ id: '', name: '', createdAt: '', updatedAt: '' });

/** Injectable provider sender for tests and local applications. */
export interface AshbyClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Ashby REST API client for candidate metadata. */
export class AshbyClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Ashby. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: AshbyClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Ashby configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Ashby request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getCandidate Query. */ getCandidate(): GetCandidateOperation { return new GetCandidateOperation(this); }

  /** Resolve the API key and send one bounded candidate lookup. */
  async get(call: Call, candidateID: string): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiKey || !isHeaderSafeCredential(apiKey) || apiKey.includes(':')) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/candidate.info`, method: 'POST',
      body: Buffer.from(JSON.stringify({ id: candidateID }), 'utf8'),
      headers: { Accept: 'application/json; version=1', 'Content-Type': 'application/json', Authorization: `Basic ${Buffer.from(apiKey + ':').toString('base64')}` },
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

/** Read one candidate's ID, name, and timestamps. */
export class GetCandidateOperation implements Query<GetCandidateInput, CandidateProfile> {
  /** Bind the provider client. */ constructor(private readonly client: AshbyClient) {}
  /** Return the generated Query candidate. */ definition(): QueryDefinition { return GetCandidateDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetCandidateInput): Promise<QueryAttempt<CandidateProfile>> {
    if (!isValidCandidateID(input?.candidateId))
      return terminal('defect', 'VALIDATION', 'candidate ID must be a UUID');
    const exchange = await this.client.get(call, input.candidateId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Ashby credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Ashby request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Ashby rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Ashby is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Ashby did not expose the candidate');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Ashby rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Ashby response exceeded the configured limit');
    const decoded = decodeCandidate(exchange.body, input.candidateId);
    if (!decoded) return terminal('invalidResponse', 'PROTOCOL', 'Ashby returned invalid candidate JSON');
    if ('errorCode' in decoded) return decoded.errorCode === 'candidate_not_found'
      ? terminal('notFound', 'NOT_FOUND', 'Ashby did not expose the candidate')
      : terminal('providerRejected', 'PROVIDER_REJECTION', 'Ashby rejected the candidate lookup');
    return newQueryBranch('found', decoded);
  }
}

function isValidCandidateID(value: unknown): value is string {
  return typeof value === 'string' && /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'ashby', operation: 'getCandidate', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<CandidateProfile> { return newQueryBranch(branch, emptyCandidate(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<CandidateProfile> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeCandidate(body: Uint8Array, candidateID: string): CandidateProfile | { errorCode: string } | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || typeof raw.success !== 'boolean') return undefined;
  if (!raw.success) return isObject(raw.errorInfo) && typeof raw.errorInfo.code === 'string' && raw.errorInfo.code.trim()
    ? { errorCode: raw.errorInfo.code } : undefined;
  if (!isObject(raw.results)) return undefined;
  const data = raw.results;
  if (data.id !== candidateID || typeof data.name !== 'string' || !data.name.trim() ||
    typeof data.createdAt !== 'string' || !data.createdAt.trim() ||
    typeof data.updatedAt !== 'string' || !data.updatedAt.trim()) return undefined;
  return { id: candidateID, name: data.name, createdAt: data.createdAt, updatedAt: data.updatedAt };
}
