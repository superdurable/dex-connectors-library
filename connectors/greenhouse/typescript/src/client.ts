// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, SecretString, newQueryBranch, newQueryRetry, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCandidateDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { CandidateProfile, Config, CredentialSource, Credentials, GetCandidateInput } from './generated/models.js';

const defaultBaseURL = 'https://harvest.greenhouse.io/v3';
const emptyProfile = (): CandidateProfile => ({ id: 0, firstName: '', lastName: '' });

/** Injectable provider sender for tests and local applications. */
export interface GreenhouseClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Greenhouse Harvest v3 client for selected candidate fields. */
export class GreenhouseClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Greenhouse', auth: Authorization, credentialFields: CredentialFields });

  /** Validate Harvest settings without contacting Greenhouse. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: GreenhouseClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Greenhouse configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL).replace(/\/$/, '');
    if (!new URL(this.baseURL).pathname.endsWith('/v3')) throw new TypeError('Greenhouse Harvest URL must end in /v3');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Greenhouse request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getCandidate Query. */ getCandidate(): GetCandidateOperation { return new GetCandidateOperation(this); }

  /** Resolve the renewable token and perform one bounded candidate lookup. */
  async get(call: Call, candidateID: number): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch (error) { return emptyExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'request'); }
    if (!isHeaderSafeCredential(token)) return emptyExchange('credential');
    for (let attempt = 0; attempt < 2; attempt++) {
      const exchange = await this.sendCandidateRequest(call, token, candidateID);
      if (exchange.status !== 401 || attempt > 0 || !('resolveAfterRejection' in this.credentials)) return exchange;
      try {
        const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
        token = replacement.accessToken instanceof SecretString ? replacement.accessToken.reveal() : '';
      } catch (error) { return error instanceof ReauthorizationRequiredError ? exchange : emptyExchange('request'); }
      if (!isHeaderSafeCredential(token)) return exchange;
    }
    return emptyExchange('request');
  }

  private async sendCandidateRequest(call: Call, token: string, candidateID: number): Promise<Exchange> {
    const query = new URLSearchParams({ fields: 'id,first_name,last_name', ids: String(candidateID), per_page: '1' });
    const request: ProviderRequest = {
      url: `${this.baseURL}/candidates?${query}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
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

/** Read one candidate's ID and names from Greenhouse. */
export class GetCandidateOperation implements Query<GetCandidateInput, CandidateProfile> {
  /** Bind one Greenhouse client. */ constructor(private readonly client: GreenhouseClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetCandidateDefinition; }
  /** Classify candidate lookup outcomes consistently with Go. */
  async invoke(call: Call, input: GetCandidateInput): Promise<QueryAttempt<CandidateProfile>> {
    if (!Number.isSafeInteger(input?.candidateId) || input.candidateId <= 0)
      return terminal('defect', 'VALIDATION', 'candidate ID must be positive');
    const exchange = await this.client.get(call, input.candidateId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Greenhouse access token is unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Greenhouse request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Greenhouse rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Greenhouse is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Greenhouse did not expose the candidate');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Greenhouse rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Greenhouse response exceeded the configured limit');
    const decoded = decodeCandidate(exchange.body, input.candidateId);
    if (decoded === undefined) return terminal('invalidResponse', 'PROTOCOL', 'Greenhouse returned invalid candidate JSON');
    if (decoded === null) return terminal('notFound', 'NOT_FOUND', 'Greenhouse did not expose the candidate');
    return newQueryBranch('found', decoded);
  }
}

function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'greenhouse', operation: 'getCandidate', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<CandidateProfile> { return newQueryBranch(branch, emptyProfile(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<CandidateProfile> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeCandidate(body: Uint8Array, candidateID: number): CandidateProfile | null | undefined {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!Array.isArray(raw) || raw.length > 1) return undefined;
  if (raw.length === 0) return null;
  const candidate: unknown = raw[0];
  if (!isObject(candidate) || candidate.id !== candidateID) return undefined;
  const firstName = candidate.first_name;
  const lastName = candidate.last_name;
  if (firstName !== null && firstName !== undefined && typeof firstName !== 'string') return undefined;
  if (lastName !== null && lastName !== undefined && typeof lastName !== 'string') return undefined;
  const first = typeof firstName === 'string' ? firstName : '';
  const last = typeof lastName === 'string' ? lastName : '';
  if (!first.trim() && !last.trim()) return undefined;
  return { id: candidateID, firstName: first, lastName: last };
}
