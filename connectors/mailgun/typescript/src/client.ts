// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials, DomainsOutput, EventsOutput, GetEventsInput, ListDomainsInput } from './generated/models.js';
import { GetEventsDefinition, ListDomainsDefinition } from './generated/definitions.js';

const defaultBaseUrl = 'https://api.mailgun.net';
const domainPattern = /^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$/;

/** Injectable HTTP sender for provider and conformance tests. */
export type MailgunRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for deterministic tests. */
export interface MailgunClientOptions { /** Send one bounded HTTP request. */ send?: MailgunRequestSender }

type WireResult<Output> = { response: ProviderResponse } | { attempt: QueryAttempt<Output> };

/** Mailgun HTTP client with per-call credential resolution. */
export class MailgunClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: MailgunRequestSender;

  /** Validate settings and retain a credential provider without contacting Mailgun. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MailgunClientOptions = {}) {
    if (!credentials) throw new TypeError('Mailgun credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Mailgun timeout must be positive');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Mailgun maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the domain events Query. */
  getEvents(): GetEventsOperation { return new GetEventsOperation(this); }
  /** Bind the account domains Query. */
  listDomains(): ListDomainsOperation { return new ListDomainsOperation(this); }

  /** Resolve private credentials, send once, and classify common HTTP failures. */
  async query<Output>(call: Call, operation: string, path: string, query: URLSearchParams, zero: Output): Promise<WireResult<Output>> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiKey instanceof SecretString)) throw new TypeError('invalid Mailgun key');
      apiKey = credentials.apiKey.reveal();
      if (!isHeaderSafeCredential(apiKey)) throw new TypeError('invalid Mailgun key');
    } catch { return { attempt: failed(operation, 'defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid', zero) }; }
    const suffix = query.size ? `?${query.toString()}` : '';
    try {
      const response = await this.send({
        url: `${this.baseUrl}${path}${suffix}`, method: 'GET',
        headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`api:${apiKey}`, 'utf8').toString('base64')}` },
        maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: this.baseUrl === defaultBaseUrl || this.baseUrl === 'https://api.eu.mailgun.net' ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
      const classified = classifyStatus(operation, response.status, response.headers['retry-after'], zero);
      return classified ? { attempt: classified } : { response };
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        const classified = error.status === undefined ? undefined : classifyStatus(operation, error.status, error.headers?.['retry-after'], zero);
        return { attempt: classified ?? failed(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit', zero) };
      }
      return { attempt: newQueryRetry(failure(operation, 'AVAILABILITY', 'provider request failed')) };
    }
  }
}

/** Query one bounded page of events for a sending domain. */
export class GetEventsOperation {
  /** Bind a validated Mailgun client. */ constructor(private readonly client: MailgunClient) {}
  /** Return the stable manifest contract. */ definition() { return GetEventsDefinition; }
  /** Validate the domain, make one provider request, and decode its page. */
  async invoke(call: Call, input: GetEventsInput): Promise<QueryAttempt<EventsOutput>> {
    if (domainPattern.exec(input.domain)?.[0] !== input.domain || !Number.isInteger(input.limit ?? 0) || (input.limit ?? 0) < 0 || (input.limit ?? 0) > 300)
      return failed('getEvents', 'defect', 'VALIDATION', 'domain or limit is invalid', { events: [] });
    const query = new URLSearchParams();
    if (input.limit) query.set('limit', String(input.limit));
    const result = await this.client.query(call, 'getEvents', `/v3/${input.domain}/events`, query, { events: [] } as EventsOutput);
    if ('attempt' in result) return result.attempt;
    try {
      const wire: unknown = JSON.parse(Buffer.from(result.response.body).toString('utf8'));
      if (!isObject(wire) || !Array.isArray(wire.items)) throw new Error('events missing');
      const events = wire.items.map(value => {
        if (!isObject(value)) throw new Error('invalid event');
        return { id: goString(value.id), event: goString(value.event), timestamp: goNumber(value.timestamp) };
      });
      return newQueryBranch('listed', { events });
    } catch { return failed('getEvents', 'invalidResponse', 'PROTOCOL', 'provider returned an invalid response', { events: [] }); }
  }
}

/** Query one bounded page of account sending domains. */
export class ListDomainsOperation {
  /** Bind a validated Mailgun client. */ constructor(private readonly client: MailgunClient) {}
  /** Return the stable manifest contract. */ definition() { return ListDomainsDefinition; }
  /** Validate pagination, make one provider request, and decode the page. */
  async invoke(call: Call, input: ListDomainsInput): Promise<QueryAttempt<DomainsOutput>> {
    if (!Number.isInteger(input.limit ?? 0) || (input.limit ?? 0) < 0 || (input.limit ?? 0) > 1000 || !Number.isInteger(input.skip ?? 0) || (input.skip ?? 0) < 0)
      return failed('listDomains', 'defect', 'VALIDATION', 'limit or skip is invalid', { domains: [], totalCount: 0 });
    const query = new URLSearchParams();
    if (input.limit) query.set('limit', String(input.limit));
    if (input.skip) query.set('skip', String(input.skip));
    const result = await this.client.query(call, 'listDomains', '/v4/domains', query, { domains: [], totalCount: 0 } as DomainsOutput);
    if ('attempt' in result) return result.attempt;
    try {
      const wire: unknown = JSON.parse(Buffer.from(result.response.body).toString('utf8'));
      if (!isObject(wire) || !Array.isArray(wire.items) || typeof wire.total_count !== 'number' || !Number.isInteger(wire.total_count)) throw new Error('domains missing');
      const domains = wire.items.map(value => {
        if (!isObject(value)) throw new Error('invalid domain');
        return { name: goString(value.name), state: goString(value.state) };
      });
      return newQueryBranch('listed', { domains, totalCount: wire.total_count });
    } catch { return failed('listDomains', 'invalidResponse', 'PROTOCOL', 'provider returned an invalid response', { domains: [], totalCount: 0 }); }
  }
}

function classifyStatus<Output>(operation: string, status: number, retryAfter: string | string[] | undefined, zero: Output): QueryAttempt<Output> | undefined {
  const delay = parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '');
  if (status === 429) return newQueryRetry(failure(operation, 'RATE_LIMIT', 'provider rate limited the request'), delay);
  if (status === 408 || status >= 500) return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider is temporarily unavailable'), delay);
  if (status !== 200) return failed(operation, 'providerRejected', 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${status}`, zero);
  return undefined;
}
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'mailgun', operation, message }; }
function failed<T>(operation: string, branch: string, kind: Failure['kind'], message: string, value?: T): QueryAttempt<T> {
  return newQueryBranch(branch, value as T, {}, failure(operation, kind, message));
}
function isObject(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function goString(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new Error('expected string'); return value; }
function goNumber(value: unknown): number { if (value === undefined || value === null) return 0; if (typeof value !== 'number') throw new Error('expected number'); return value; }
