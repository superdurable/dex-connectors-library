// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, SecretString, newQueryBranch, newQueryRetry, resolveCredential, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetPersonDefinition, ListPersonsDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { PersonProfile, Config, CredentialSource, Credentials, GetPersonInput, ListPersonsInput, PersonPage } from './generated/models.js';

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
  /** Validated Personio v2 base URL used to check absolute pagination links. */
  readonly baseURL: string;
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
    return this.requestPersons(call, this.baseURL + '/persons/' + personID);
  }

  /** Bind the cursor-paginated persons Query. */
  listPersons(): ListPersonsOperation { return new ListPersonsOperation(this); }

  /** Send the supplied filter parameters to the configured persons collection. */
  async requestPersonPage(call: Call, filters: URLSearchParams): Promise<Exchange> {
    const query = filters.toString();
    return this.requestPersons(call, this.baseURL + '/persons' + (query ? '?' + query : ''));
  }

  private async requestPersons(call: Call, url: string): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch (error) { return emptyExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'request'); }
    if (!isHeaderSafeCredential(token)) return emptyExchange('credential');
    return this.sendPersonRequest(call, token, url);
  }

  private async sendPersonRequest(call: Call, token: string, url: string): Promise<Exchange> {
    const request: ProviderRequest = {
      url, method: 'GET',
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
      return terminal('getPerson',emptyProfile(),'defect', 'VALIDATION', 'Personio person ID must be an API ID');
    const exchange = await this.client.get(call, input.personId);
    const issue = classifyExchange(exchange, 'getPerson', emptyProfile());
    if (issue) return issue;
    const decoded = decodePerson(exchange.body, input.personId);
    if (decoded === undefined) return terminal('getPerson',emptyProfile(),'invalidResponse', 'PROTOCOL', 'Personio returned invalid person JSON');
    return newQueryBranch('found', decoded);
  }
}

function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function terminal<T>(
  operation: string,
  value: T,
  branch: string,
  kind: Failure['kind'],
  message: string,
): QueryAttempt<T> {
  return newQueryBranch(branch, value, {}, { provider: 'personio', operation, kind, message });
}
function classifyExchange<T>(exchange: Exchange, operation: string, empty: T): QueryAttempt<T> | undefined {
  const reject = (branch: string, kind: Failure['kind'], message: string) =>
    terminal(operation, empty, branch, kind, message);
  const retry = (kind: Failure['kind'], message: string, delay = 0): QueryAttempt<T> =>
    newQueryRetry({ provider: 'personio', operation, kind, message }, delay);
  if (exchange.error === 'credential')
    return reject('defect', 'AUTHENTICATION', 'Personio connection needs authorization');
  if (exchange.error === 'request') return retry('TRANSPORT', 'Personio request failed');
  if (exchange.status === 429)
    return retry('RATE_LIMIT', 'Personio rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500)
    return retry('AVAILABILITY', 'Personio is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status === 404 && operation === 'getPerson')
    return reject('notFound', 'NOT_FOUND', 'Personio person was not found');
  if (exchange.status !== 200)
    return reject('providerRejected', 'PROVIDER_REJECTION', `Personio rejected HTTP ${exchange.status}`);
  if (exchange.error === 'tooLarge')
    return reject('invalidResponse', 'RESPONSE_TOO_LARGE', 'Personio response exceeded the configured limit');
  return undefined;
}
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(entry) ? (entry[0] ?? '') : (entry ?? '');
}
function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function decodePerson(body: Uint8Array, personID: string): PersonProfile | undefined {
  let raw: unknown;
  try {
    raw = readDocument(body);
  } catch {
    return undefined;
  }
  if (!isObject(raw) || raw.id !== personID) return undefined;
  const firstName = raw.first_name;
  const lastName = raw.last_name;
  if (firstName !== null && firstName !== undefined && typeof firstName !== 'string') return undefined;
  if (lastName !== null && lastName !== undefined && typeof lastName !== 'string') return undefined;
  const first = typeof firstName === 'string' ? firstName : '';
  const last = typeof lastName === 'string' ? lastName : '';
  return { id: personID, firstName: first, lastName: last };
}

function readDocument(body: Uint8Array): unknown {
  return JSON.parse(new TextDecoder().decode(body), (_, value: unknown) =>
    isObject(value)
      ? Object.fromEntries(Object.entries(value).map(([key, field]) => [key.toLowerCase(), field]))
      : value,
  ) as unknown;
}

/** Return selected person IDs and names from one filtered Personio page. */
export class ListPersonsOperation implements Query<ListPersonsInput, PersonPage> {
  /** Bind the existing authenticated client and its bounded HTTP transport. */
  constructor(private readonly client: PersonioClient) {}
  /** Return the generated collection Query contract. */
  definition(): QueryDefinition {
    return ListPersonsDefinition;
  }
  /** Read one page and extract the opaque cursor from its continuation link. */
  async invoke(call: Call, input: ListPersonsInput): Promise<QueryAttempt<PersonPage>> {
    const empty: PersonPage = { persons: [], nextCursor: '' };
    const limit = input?.limit ?? 0,
      cursor = input?.cursor ?? '',
      first = input?.firstName ?? '',
      last = input?.lastName ?? '',
      status = input?.status ?? '';
    if (
      !input ||
      !Number.isSafeInteger(limit) ||
      limit < 0 ||
      limit > 50 ||
      typeof cursor !== 'string' ||
      Buffer.byteLength(cursor) > 1024 ||
      typeof first !== 'string' ||
      typeof last !== 'string' ||
      !['', 'ACTIVE', 'INACTIVE'].includes(status)
    )
      return terminal('listPersons', empty, 'defect', 'VALIDATION', 'Personio list filters are invalid');
    const filters = new URLSearchParams();
    for (const [key, value] of Object.entries({
      cursor,
      first_name: first,
      last_name: last,
      limit: limit ? String(limit) : '',
      status,
    }))
      if (value) filters.set(key, value);
    const exchange = await this.client.requestPersonPage(call, filters);
    const issue = classifyExchange(exchange, 'listPersons', empty);
    if (issue) return issue;
    try {
      return newQueryBranch('listed', readPage(exchange.body, this.client.baseURL));
    } catch {
      return terminal(
        'listPersons',
        empty,
        'invalidResponse',
        'PROTOCOL',
        'Personio returned invalid persons JSON',
      );
    }
  }
}
function readPage(body: Uint8Array, baseURL: string): PersonPage {
  const doc = readDocument(body);
  if (!isObject(doc) || !Array.isArray(doc._data)) throw new Error('Missing persons');
  const persons: PersonProfile[] = doc._data.map((row: unknown) => {
    if (!isObject(row) || typeof row.id !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(row.id))
      throw new Error('Invalid person ID');
    const firstName = row.first_name ?? '',
      lastName = row.last_name ?? '';
    if (typeof firstName !== 'string' || typeof lastName !== 'string') throw new Error('Invalid person name');
    return { id: row.id, firstName, lastName };
  });
  let links = doc;
  for (const key of ['_meta', 'links', 'next']) {
    const field: unknown = links[key] ?? {};
    if (!isObject(field)) throw new Error('Invalid continuation metadata');
    links = field;
  }
  const href = links.href ?? '';
  if (typeof href !== 'string') throw new Error('Invalid continuation link');
  let nextCursor = '';
  if (href) {
    if (Array.from(href).some((char) => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127))
      throw new Error('Invalid continuation link');
    const absolute = /^[A-Za-z][A-Za-z0-9+.-]*:/.test(href);
    const base = new URL(baseURL),
      next = new URL(href, base.origin);
    const path = href.split(/[?#]/, 1)[0]!.replace(/^(?:[a-z][a-z0-9+.-]*:)?\/\/[^/]*/i, '');
    if (decodeURIComponent(path) !== '/v2/persons' || (absolute && next.origin !== base.origin))
      throw new Error('Invalid continuation link');
    const pairs = next.search
      .slice(1)
      .split('&')
      .filter((pair) => !pair.includes(';') && !/%(?![0-9a-fA-F]{2})/.test(pair));
    nextCursor = new URLSearchParams(pairs.join('&')).get('cursor') ?? '';
    if (!nextCursor) throw new Error('Missing continuation cursor');
  }
  return { persons, nextCursor };
}
