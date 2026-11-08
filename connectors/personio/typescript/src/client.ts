// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, SecretString, newQueryBranch, newQueryRetry, resolveCredential, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetPersonDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { PersonProfile, Config, CredentialSource, Credentials, GetPersonInput } from './generated/models.js';

const defaultBaseURL = 'https://api.personio.de/v2';
const emptyProfile = (): PersonProfile => ({ id: '', firstName: '', lastName: '' });

/** Injectable provider sender for tests and local applications. */
export interface PersonioClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Personio v2 client for selected person fields. */
export class PersonioClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Personio', auth: Authorization, credentialFields: CredentialFields });

  /** Validate Personio settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: PersonioClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Personio configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL).replace(/\/$/, '');
    if (new URL(this.baseURL).pathname !== '/v2') throw new TypeError('Personio API URL must end in /v2');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Personio request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getPerson Query. */ getPerson(): GetPersonOperation { return new GetPersonOperation(this); }

  /** Resolve the renewable token and perform one bounded person lookup. */
  async get(call: Call, personID: string): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch (error) { return emptyExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'request'); }
    if (!isHeaderSafeCredential(token)) return emptyExchange('credential');
    return this.sendPersonRequest(call, token, personID);
  }

  private async sendPersonRequest(call: Call, token: string, personID: string): Promise<Exchange> {
    const request: ProviderRequest = {
      url: `${this.baseURL}/persons/${personID}`, method: 'GET',
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

/** Read one person's ID and names from Personio. */
export class GetPersonOperation implements Query<GetPersonInput, PersonProfile> {
  /** Bind one Personio client. */ constructor(private readonly client: PersonioClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetPersonDefinition; }
  /** Classify person lookup outcomes consistently with Go. */
  async invoke(call: Call, input: GetPersonInput): Promise<QueryAttempt<PersonProfile>> {
    if (typeof input?.personId !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(input.personId))
      return terminal('defect', 'VALIDATION', 'Personio person ID must be an API ID');
    const exchange = await this.client.get(call, input.personId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Personio connection needs authorization');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Personio request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Personio rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Personio is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Personio person was not found');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Personio rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Personio response exceeded the configured limit');
    const decoded = decodePerson(exchange.body, input.personId);
    if (decoded === undefined) return terminal('invalidResponse', 'PROTOCOL', 'Personio returned invalid person JSON');
    return newQueryBranch('found', decoded);
  }
}

function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'personio', operation: 'getPerson', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<PersonProfile> { return newQueryBranch(branch, emptyProfile(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<PersonProfile> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodePerson(body: Uint8Array, personID: string): PersonProfile | undefined {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!isObject(raw) || raw.id !== personID) return undefined;
  const firstName = raw.first_name;
  const lastName = raw.last_name;
  if (firstName !== null && firstName !== undefined && typeof firstName !== 'string') return undefined;
  if (lastName !== null && lastName !== undefined && typeof lastName !== 'string') return undefined;
  const first = typeof firstName === 'string' ? firstName : '';
  const last = typeof lastName === 'string' ? lastName : '';
  return { id: personID, firstName: first, lastName: last };
}
